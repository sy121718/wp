package productservice

// product_bulk_pricing.go — 批量改价用例（task-156，dd.txt §5.1/§5.3/§5.4）。
//
// 与另外五个批量删除一样，这一个也**整批一个事务**：勾选的商品要么全改、要么一个都不改。
// 任何一条不能处理（商品不在了 / 算价失败）就把整批退回，并在 data.items 里列出可定位的条目。
//
// 一条改价 = 一条 price_adjustments 台账（含逐变体「原价 → 新价」明细）+ 一次按商品的变体留痕 ——
// 这个**按商品的粒度保持不变**：把 N 个商品合并成一条台账会让「按商品查调价历史」失去意义，
// 也会让批次的 TargetID 语义含混。变的只是事务边界：N 条台账在同一个事务里写。
//
// 为什么必须整批：改价是不可逆的经营动作，而「成功 3 个、失败 2 个」留下的是**一半新价、
// 一半旧价**的商品集合，用户拿到的数字没有任何东西能修回去（判据见 internal/module/CLAUDE.md
// 「列表页批量操作」）。所以这里没有 skipped、没有逐项回执：成功只回一条结论 + 计数。
//
// 循环里该批量的仍然批量：存在性检查一次查回整批，自动标签重算整批只跑一次。

import (
	"context"
	"net/http"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
)

// BulkApplyPricing 批量按规则改价（POST /api/product/bulk-pricing 的 service 入口）。
//
// 规则的作用范围恒为「单个商品的全部变体」（scope=product、TargetID=当前商品 id）：
// 入口语义就是「对勾选的这几个商品各做一次改价」，调用方不必（也不该）传 Scope / TargetID。
func (s *Service) BulkApplyPricing(ctx context.Context, req *productdto.BulkPricingReq) (res *productdto.BulkPricingResultResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	ids, nerr := normalizeBulkIDs(req.IDs)
	if nerr != nil {
		return nil, bulkIDsErr(nerr)
	}
	res = &productdto.BulkPricingResultResp{}
	// 没勾选就提交：明确拒绝（不是「0 个改动」的成功回执）。判据在这里而不是入口 ——
	// 控制器只交请求体，不判断业务上「这次提交算不算空操作」。
	if len(ids) == 0 {
		return nil, facing.WithStatus(
			facing.New(productenums.BulkPricingNoneSelected, "批量改价：没有勾选任何商品，请先勾选左侧复选框再执行。"),
			http.StatusBadRequest)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	existing, err := s.m.ListProductsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(existing))
	names := make(map[string]string, len(existing))
	for _, p := range existing {
		present[p.ID] = true
		names[p.ID] = p.Name
	}
	blocked := make([]facing.BlockedItem, 0)
	for _, id := range ids {
		if !present[id] {
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrPricingTargetNotFound))
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	// 先全部算完再写：算价不落库，任何一条算不出来（例如规则参数在这批数据上无解）
	// 都能在写入之前把整批退回去。
	plans := make([]*pricingPlan, 0, len(ids))
	unchanged := 0
	for _, id := range ids {
		rule := req.Rule
		rule.ProjectID = projectID
		rule.Scope = productenums.PricingScopeProduct
		rule.TargetID = id
		plan, perr := s.computePricingPlan(ctx, &productdto.PricingApplyReq{
			PricingRuleReq: rule, Note: req.Note, OperatorID: req.OperatorID,
		})
		switch {
		case perr == nil:
			plans = append(plans, plan)
			res.ChangedVariants += plan.res.ChangedCount
		case isPricingNothingChanged(perr):
			// 本来就在目标价上：不是失败，不写空台账。整批全是这种情况时
			// 一个事务都不开（见下面的 len(plans) == 0 分支）。
			unchanged++
		case pricingUnpriceable(perr):
			// 「这个商品上没有可改价的变体」（捆绑容器只有容器价）是**这一条不能处理**，
			// 不是规则整体无解：进 blocked，让报错能定位到具体商品；其它错误（规则参数
			// 在这批数据上算不出来 / 读库失败）仍然整批退回。
			blocked = append(blocked, bulkBlocked(id, names[id], productenums.ErrPricingTargetNotFound))
		default:
			return nil, perr
		}
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	if len(plans) > 0 {
		if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
			return s.writePricingPlansTx(ctx, tx, plans)
		}); err != nil {
			return nil, err
		}
		s.bumpFragmentCache(ctx, projectID)
		// 自动标签重算：整批一次（重算是全工程全量的幂等操作，逐商品重算只是重复劳动）。
		if _, rerr := s.recalcPricingAutoTags(ctx, projectID); rerr != nil {
			return nil, rerr
		}
	}
	// Done = 请求项数：走到这里就说明整批都处理了（AGENTS「批量要么全成要么全败」）。
	//
	// 「本来就是目标价」不是失败、也不是「没处理」——它是**处理结果为无变化**，
	// 由 UnchangedProducts 单独计数（service 不写空台账，故分开报）。
	// 三个计数各答一个问题：Done 是这一批多大、ChangedVariants 是真改了几个变体、
	// UnchangedProducts 里有几个无需改动。把它们混成一个数会让运营没法核对。
	res.Done = len(ids)
	res.UnchangedProducts = unchanged
	return res, nil
}

// pricingUnpriceable 这个错是不是「该商品上没有可改价的变体」。
//
// 判据取 fact.Key（声明处的 key），不比对文案、也不靠错误串包含判断：同一句兜底文案
// 可能被别的 key 复用，按文案分流在改措辞后必然静默失效。
func pricingUnpriceable(err error) bool {
	fact, ok := facing.FactOf(err)
	return ok && fact.Business && fact.Key == productenums.ErrPricingTargetNotFound
}
