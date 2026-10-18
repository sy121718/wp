// product_variant_options.go — 后台「选哪个 SKU」下拉的数据源（一次批量）。
package productservice

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
)

// ListVariantOptions 全工程「商品 → 变体」清单。
//
// 查询次数**与商品数无关**（两次：商品清单 + 变体批量），这是本口存在的理由 ——
// 调用方原先的写法是「List 一页商品，再逐个 Get」，100 个商品就是 101 次往返，
// 而下拉本身只是一排 option。
//
// 顺序：商品按 sort / create_time（与列表页一致），变体在商品内按 sort。
// 调用方不要重排 —— 顺序在界面上有语义（同一商品的变体挨在一起）。
func (s *Service) ListVariantOptions(ctx context.Context, projectID string) ([]productcontract.VariantOption, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		// 空工程**不解释成「全部工程」**：那会让一个工程的页面列出所有工程的 SKU。
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	rows, err := s.m.ListForVariantOptions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []productcontract.VariantOption{}, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	variants, err := s.m.ListVariantsByProducts(ctx, ids)
	if err != nil {
		return nil, err
	}
	byProduct := make(map[string][]productcontract.VariantOption, len(rows))
	for _, v := range variants {
		if v == nil {
			continue
		}
		byProduct[v.ProductID] = append(byProduct[v.ProductID], productcontract.VariantOption{
			VariantID: v.ID, ProductID: v.ProductID, SKUCode: v.SKUCode, CostPrice: v.CostPrice,
		})
	}
	out := make([]productcontract.VariantOption, 0, len(variants))
	// 按商品顺序回填（变体批量查询只保证组内有序，跨商品的顺序不确定）。
	for _, r := range rows {
		for _, option := range byProduct[r.ID] {
			option.ProductName = r.Name
			out = append(out, option)
		}
	}
	return out, nil
}
