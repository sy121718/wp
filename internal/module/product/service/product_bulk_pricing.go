package productservice

// product_bulk_pricing.go — 批量改价用例（task-156，dd.txt §5.1/§5.3/§5.4）。
//
// 与另外五个批量删除不同，这一个**必须保留逐商品循环**：一次改价 = 一条 price_adjustments
// 台账（含逐变体「原价 → 新价」明细）+ 一次按商品的变体留痕；把 N 个商品合并成一条台账，
// 会让「按商品查调价历史」失去意义，也会让批次的 TargetID 语义含混。
// dd.txt §5.4 允许这类保留（循环在 service 里、并写明业务原因），但要求**循环外该批量的都批量**：
//   · 存在性检查：一次 ListProductsByIDs 查回整批（现状是逐条 ApplyPricing 各自查一遍，
//     不存在的商品也要走完一整次调用才发现）；
//   · 自动标签重算：整批结束后只跑一次（现状 N 个商品触发 N 次全工程全量重算）。
//
// 事务语义：每个商品的「写回价格 + 写台账 + 发失效 + 留痕」仍是**一个独立事务**
//（applyPricingOnce 内部），与改造前逐商品调用完全一致 —— 一条失败不影响其它条。
// 这里不做「整批一个事务」：改价是不可逆的经营动作，用户要的正是「成功几个、跳过几个」，
// 而且一次给 200 个商品的价格加行锁会把并发改价彻底串行化。

import (
	"context"
	"errors"
	"strings"

	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
)

// BulkApplyPricing 批量按规则改价（POST /admin/products/bulk-pricing 的 service 入口）。
//
// 规则的作用范围恒为「单个商品的全部变体」（scope=product、TargetID=当前商品 id）：
// 入口语义就是「对勾选的这几个商品各做一次改价」，调用方不必（也不该）传 Scope / TargetID。
func (s *Service) BulkApplyPricing(ctx context.Context, req *productdto.BulkPricingReq) (res *productdto.BulkPricingResult, err error) {
	if req == nil {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	ids := normalizeBulkIDs(req.IDs)
	res = &productdto.BulkPricingResult{BulkResult: productdto.BulkResult{Requested: len(ids)}}
	if len(ids) == 0 {
		return res, nil
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
	for _, p := range existing {
		present[p.ID] = true
	}
	// 逐条回执：基座按 outcome=update 读 item.updated（与删除读 item.deleted 同形）。
	res.Items = make([]productdto.BulkItem, 0, len(ids))
	for _, id := range ids {
		if !present[id] {
			// 商品不存在（或不属于这个工程）：与改造前一致 —— ApplyPricing 会返回
			// ErrPricingTargetNotFound，控制器把它算进「跳过」。
			res.Skipped++
			if res.SkipReasonKey == "" {
				res.SkipReasonKey = productenums.ErrPricingTargetNotFound
			}
			res.Items = append(res.Items, productdto.BulkItem{ID: id, ReasonKey: productenums.ErrPricingTargetNotFound})
			continue
		}
		rule := req.Rule
		rule.ProjectID = projectID
		rule.Scope = productenums.PricingScopeProduct
		rule.TargetID = id
		ap, aerr := s.applyPricingOnce(ctx, &productdto.PricingApplyReq{
			PricingRuleReq: rule, Note: req.Note, OperatorID: req.OperatorID,
		})
		switch {
		case aerr == nil:
			res.Succeeded++
			if ap != nil {
				res.ChangedVariants += ap.ChangedCount
			}
			res.Items = append(res.Items, productdto.BulkItem{ID: id, Updated: true})
		case strings.TrimSpace(aerr.Error()) == productenums.ErrPricingNothingChanged:
			// 本来就在目标价上：不是失败（service 不写空台账），单独计数。
			// 回执里仍算「改过」—— 否则基座会把这条列进失败原因（它没错，只是无需改动）。
			res.UnchangedProducts++
			res.Items = append(res.Items, productdto.BulkItem{ID: id, Updated: true})
		default:
			res.Skipped++
			if res.SkipReasonKey == "" {
				res.SkipReasonKey = bulkSkipReasonKey(aerr)
			}
			res.Items = append(res.Items, productdto.BulkItem{ID: id, ReasonKey: bulkSkipReasonKey(aerr)})
		}
	}
	// 自动标签重算：整批一次（重算是全工程全量的幂等操作，逐商品重算只是重复劳动）。
	// 只有真改了价才重算 —— 改造前也是这个时机（一条改动都没有的那次调用早退了）。
	if res.ChangedVariants > 0 {
		if _, rerr := s.recalcPricingAutoTags(ctx, projectID); rerr != nil {
			return nil, rerr
		}
	}
	// 注意 Failed 恒为 0：改造前控制器把所有非「无改动」的错误统一算进「跳过 M 个商品」，
	// 批量用例保持同一个口径 —— 把内部错误从「跳过」里拆出来单独显示会改变用户看到的
	// 数字，而提示页只有「改价 N 个变体 / 跳过 M 个商品」两个数字。
	return res, nil
}

// bulkSkipReasonKey 从一次跳过里取**业务 key**（enums 常量值），而不是成品串。
//
// 错误形态两种：errors.New(key) 与 fmt.Errorf("%s：%s", key, 明细)（后者见
// crossProjectRefBlocked）。成品串要按请求语言取词，而取词只在 inbound 有
// （productErrText）—— 所以 service 只回 key，由 inbound 翻成当前语言的整句。
func bulkSkipReasonKey(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if key, _, ok := strings.Cut(msg, "："); ok {
		return strings.TrimSpace(key)
	}
	return msg
}
