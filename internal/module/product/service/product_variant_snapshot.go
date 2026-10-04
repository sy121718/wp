package productservice

// product_variant_snapshot.go — 订单域用的变体快照（只读，实现 VariantSnapshotPort）。

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productcontract "go_wp/internal/module/product/contract"
)

// 编译期断言：本 service 满足订单域需要的快照端口。
var _ productcontract.VariantSnapshotPort = (*Service)(nil)

// VariantSnapshots 按变体 id 批量取下单快照需要的事实。只读、无副作用。
//
// projectID 是必填的工程作用域（审计 DB-009）：见端口与 model.ListByIDs 的注释。
func (s *Service) VariantSnapshots(ctx context.Context, variantIDs []string, projectID string) (list []*productcontract.VariantSnapshot, err error) {
	ids := dedupeNonEmpty(variantIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	variants, err := s.m.ListVariantsByIDs(ctx, ids, projectID)
	if err != nil {
		return nil, err
	}
	if len(variants) == 0 {
		return nil, nil
	}
	// 变体表没有商品名与工程列，按 product_id 批量补齐（不逐条查，避免 N+1）
	productIDs := make([]string, 0, len(variants))
	seen := make(map[string]bool, len(variants))
	for _, v := range variants {
		if !seen[v.ProductID] {
			seen[v.ProductID] = true
			productIDs = append(productIDs, v.ProductID)
		}
	}
	// 商品名与工程一并补齐：作用域与变体那次读取同源（端口入参的 projectID），
	// 不在这里另取工程上下文 —— 消费方给的工程就是唯一真源。
	products, err := s.m.ListByIDs(ctx, productIDs, projectID)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[string]string, len(products))
	projectOf := make(map[string]string, len(products))
	for _, p := range products {
		nameOf[p.ID] = p.Name
		projectOf[p.ID] = p.ProjectID
	}
	// 成本来自 **(仓库, SKU)**：该变体在归属仓（未指定仓库时按库存域既有的归属仓解析
	// 规则 —— 默认仓）的当前成本。成本搬到仓库之后，变体级 product_variants.cost_price
	// 不再是订单成本快照的来源（docs/14 §9.3 的 ⚠️：沿用变体级会让订单利润与仓库侧
	// 对不上，且改价后历史利润会漂移）。
	refs := make([]inventorycontract.VariantWarehouseCostRef, 0, len(variants))
	for _, v := range variants {
		refs = append(refs, inventorycontract.VariantWarehouseCostRef{VariantID: v.ID})
	}
	costs := s.variantWarehouseCosts(ctx, projectID, refs)

	list = make([]*productcontract.VariantSnapshot, 0, len(variants))
	for _, v := range variants {
		// 商品行在本工程作用域内不可见 ⇒ 该变体不属于本工程，**丢掉它**。
		//
		// 这一步不能省，也不能改成「照旧返回一条 ProductName / ProjectID 为空的快照」：
		// 变体表（product_variants）不在迁移 215 的名单里、没有策略，所以别的工程的变体 id
		// 会被照常读出来；若把它交给消费方，order / cart 那道
		// 「sn.ProjectID != "" && sn.ProjectID != projectID」的守卫会因为 ProjectID 为空而
		// **放行** —— 那是用一次静默降级换掉一条越权拦截（订单会落一行商品名为空的快照项）。
		//
		// 丢掉之后，消费方看到的现象与「这个规格不存在」完全一致 —— 端口契约本来就这么写
		// （查不到的 id 不出现在返回里），调用方按差集判定「不存在或已删除」。
		ownerProject := projectOf[v.ProductID]
		if ownerProject == "" {
			continue
		}
		list = append(list, &productcontract.VariantSnapshot{
			VariantID:    v.ID,
			ProductID:    v.ProductID,
			ProjectID:    ownerProject,
			ProductName:  nameOf[v.ProductID],
			VariantLabel: variantOptionLabel(v.OptionValues),
			SKU:          v.SKUCode,
			Price:        yuanToCents(v.Price),
			CostPrice:    costOf(costs, v.ID),
			Enabled:      v.Enabled,
		})
	}
	return list, nil
}

// variantOptionLabel 把 option_values（属性组 key → 属性值 key）拼成一行规格文本。
//
// 用值 key 而不是属性值表里的展示 label：快照要的是**下单那一刻看到的那套组合**，
// 现去查属性值表还会把「事后改了 label」带进来。只有一处例外要注意 ——
// 值 key 是英文/拼音时读起来不如中文 label，展示层需要更好看的名字时应另做映射，
// 而不是让快照去依赖一张会变的表。
func variantOptionLabel(raw json.RawMessage) string {
	pairs := decodeOptionPairs(raw)
	if len(pairs) == 0 {
		return ""
	}
	vals := make([]string, 0, len(pairs))
	for _, p := range pairs {
		if strings.TrimSpace(p.Value) != "" {
			vals = append(vals, p.Value)
		}
	}
	return strings.Join(vals, " / ")
}

// yuanToCents 元 → 分。商品域是 numeric(12,2)（元），订单域一律整数分。
func yuanToCents(yuan float64) int64 {
	return int64(math.Round(yuan * 100))
}

// costOf 取某个变体的成本快照（分）；没有仓库侧成本时返回 nil（= 尚未核算）。
//
// 返回指针而不是哨兵值：0 是合法的显式成本（赠品 / 内部划拨），拿 0 冒充「未知」
// 会让订单利润凭空多出一笔；用负值当哨兵则把「契约能否表达未知」藏进实现侧注释里。
func costOf(costs map[string]int64, variantID string) *int64 {
	cents, ok := costs[variantID]
	if !ok {
		return nil
	}
	value := cents
	return &value
}

// variantWarehouseCosts 取这些变体在**各自归属仓**的当前成本（分）。
//
// 返回的 map 只含确实有成本的变体：未核算（cost_price IS NULL）与「该仓没有这条
// 库存行」都不进 map，由 costOf 统一落成 nil。
//
// 解析失败为什么不升级成错误：价与启用态才是快照的主事实（下单 / 加购 / 实时价片段
// 三个消费方都靠它），成本是附加的记账事实；而它的失败形态（工程没有默认仓、库存行
// 还没生成）恰恰就是「尚未核算」的常见样子。把它变成错误会让「没有默认仓的工程连
// 加购都点不动」—— 那不是本次口径收口要换来的行为。成本真缺了会在订单行上表现为
// NULL，而不是被 0 掩盖。
//
// 注意**读的是当前成本**：这不是「历史成本」，历史成本由出库流水的 unit_cost 留痕
// （迁移 256）；本函数只负责下单那一刻的取值。
func (s *Service) variantWarehouseCosts(ctx context.Context, projectID string, refs []inventorycontract.VariantWarehouseCostRef) map[string]int64 {
	out := make(map[string]int64, len(refs))
	if len(refs) == 0 {
		return out
	}
	if s.invSvc == nil {
		// 未注入库存用例（纯商品单测路径）：没有库存域就没有仓库侧成本可言 ⇒ 全部未知。
		// 生产装配下 SetInventoryService 是 required-port（wiring 自检漏接即 panic）。
		return out
	}
	costs, err := s.invSvc.ResolveVariantWarehouseCosts(ctx, projectID, refs)
	if err != nil {
		return out
	}
	for _, c := range costs {
		if c.CostPrice == nil {
			continue
		}
		out[c.VariantID] = yuanToCents(*c.CostPrice)
	}
	return out
}

// dedupeNonEmpty 去空去重，保持首次出现顺序。
func dedupeNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
