package productservice

// variant_warehouse_skus.go — 变体 ↔ 仓库货行的双向查询（迁移 637，契约 VariantWarehouseStockPort）。
//
// 真源是 product_variants.warehouse_skus 这一列 JSON 数组。两个方向都从这里出发，
// 不额外维护反查表：关联是变体的属性，反查靠 GIN 索引（idx_product_variants_warehouse_skus）。

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

// 编译期断言。
var _ productcontract.VariantWarehouseStockPort = (*Service)(nil)

// WarehouseStockIDsByVariant 按变体列出它引用的库存行 id（契约见 VariantWarehouseStockPort）。
//
// projectID 必填：product_variants 自身没有工程列，工程隔离发生在归属商品那一步
// （与 ListVariantsByIDs 同一口径）；空工程直接拒绝，不退化成「不限工程」。
func (s *Service) WarehouseStockIDsByVariant(ctx context.Context, projectID string, variantIDs []string) (ids map[string][]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string][]string, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	list, err := s.m.ListVariantsByIDs(ctx, variantIDs, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		refs := productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs)
		if len(refs) == 0 {
			continue
		}
		stockIDs := make([]string, 0, len(refs))
		for _, r := range refs {
			stockIDs = append(stockIDs, r.StockID)
		}
		out[v.ID] = stockIDs
	}
	return out, nil
}

// VariantIDsByWarehouseStock 反查引用着这条货行的变体（契约见 VariantWarehouseStockPort）。
//
// 走 JSONB 包含查询：`warehouse_skus @> [{"stockId":"<id>"}]`。jsonb_path_ops 的 GIN
// 索引正好覆盖这个形态（迁移 637 建的索引就是为它）。**不是**全表扫 —— 反查在
// 「删货行 / 删仓 / 删变体」三条守卫的主路径上，扫全表会让守卫随商品数线性变慢。
func (s *Service) VariantIDsByWarehouseStock(ctx context.Context, projectID, stockID string) (variantIDs []string, err error) {
	projectID = strings.TrimSpace(projectID)
	stockID = strings.TrimSpace(stockID)
	if projectID == "" || stockID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	return s.m.VariantIDsByWarehouseStock(ctx, projectID, stockID)
}

// VariantIDsByWarehouseStocks 批量反查（契约见 VariantWarehouseStockPort）。
//
// 一条查询取出「引用了这批行中任意一条」的全部变体，再在内存里按 stockId 归位：
// 台账页一屏 50 行，逐行查会是 50 次往返，而列表页的首屏时间就在这上面。
func (s *Service) VariantIDsByWarehouseStocks(ctx context.Context, projectID string, stockIDs []string) (byStock map[string][]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string][]string, len(stockIDs))
	if len(stockIDs) == 0 {
		return out, nil
	}
	refs, err := s.m.VariantsByWarehouseStocks(ctx, projectID, stockIDs)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(stockIDs))
	for _, id := range stockIDs {
		wanted[id] = true
	}
	for _, r := range refs {
		for _, ref := range productmodel.DecodeWarehouseSKUs(r.WarehouseSKUs) {
			if !wanted[ref.StockID] {
				continue
			}
			out[ref.StockID] = append(out[ref.StockID], r.ID)
		}
	}
	return out, nil
}
