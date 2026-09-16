package productservice

// product_variant_snapshot.go — 订单域用的变体快照（只读，实现 VariantSnapshotPort）。

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
)

// 编译期断言：本 service 满足订单域需要的快照端口。
var _ productcontract.VariantSnapshotPort = (*Service)(nil)

// VariantSnapshots 按变体 id 批量取下单快照需要的事实。只读、无副作用。
func (s *Service) VariantSnapshots(ctx context.Context, variantIDs []string) (list []*productcontract.VariantSnapshot, err error) {
	ids := dedupeNonEmpty(variantIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	variants, err := s.m.ListVariantsByIDs(ctx, ids)
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
	// 显式例外（审计 DB-009）：本端口的契约里没有工程，见 model.ListByIDsWithoutScope
	// 的注释 —— 补作用域要动 VariantSnapshotPort 契约与三个消费方，不在这一批。
	products, err := s.m.ListByIDsWithoutScope(ctx, productIDs)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[string]string, len(products))
	projectOf := make(map[string]string, len(products))
	for _, p := range products {
		nameOf[p.ID] = p.Name
		projectOf[p.ID] = p.ProjectID
	}
	list = make([]*productcontract.VariantSnapshot, 0, len(variants))
	for _, v := range variants {
		list = append(list, &productcontract.VariantSnapshot{
			VariantID:    v.ID,
			ProductID:    v.ProductID,
			ProjectID:    projectOf[v.ProductID],
			ProductName:  nameOf[v.ProductID],
			VariantLabel: variantOptionLabel(v.OptionValues),
			SKU:          v.SKUCode,
			Price:        yuanToCents(v.Price),
			CostPrice:    yuanToCentsPtr(v.CostPrice),
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

// yuanToCentsPtr 可空价格 → 分（nil 记 0：没有成本价的商品成本就是未知，不是负数）。
func yuanToCentsPtr(yuan *float64) int64 {
	if yuan == nil {
		return 0
	}
	return yuanToCents(*yuan)
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
