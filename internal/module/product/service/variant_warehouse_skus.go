package productservice

// variant_warehouse_skus.go — 变体 ↔ 仓库货行的双向查询（迁移 637，契约 VariantWarehouseStockPort）。
//
// 真源是 product_variants.warehouse_skus 这一列 JSON 数组。两个方向都从这里出发，
// 不额外维护反查表：关联是变体的属性，反查靠 GIN 索引（idx_product_variants_warehouse_skus）。

import (
	"context"
	"strings"
	"time"

	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"

	"gorm.io/gorm"
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

// VariantProductIDs 变体 → 所属商品（契约见 VariantWarehouseStockPort）。
func (s *Service) VariantProductIDs(ctx context.Context, projectID string, variantIDs []string) (byVariant map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string]string, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	list, err := s.m.ListVariantsByIDs(ctx, variantIDs, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v != nil && v.ProductID != "" {
			out[v.ID] = v.ProductID
		}
	}
	return out, nil
}

// AttachWarehouseStockTx 把一条货行登记进变体的引用清单（契约见 VariantWarehouseStockPort）。
//
// 实现落在商品侧是因为真源就是 product_variants 这一列；库存侧只负责「我刚建了/用到了
// 这一行」这一个事实。整条路径只写一列（不 Save 整行）：调用方的事务里可能同时在建库存行，
// 用旧快照整行写回会把别的列冲掉。
//
// 幂等：同一 stockId 重复登记只更新线索字段（UpsertWarehouseSKURef 按 stockId 判重）。
func (s *Service) AttachWarehouseStockTx(ctx context.Context, tx *gorm.DB, projectID, variantID string, ref productcontract.WarehouseStockRef) error {
	projectID = strings.TrimSpace(projectID)
	variantID = strings.TrimSpace(variantID)
	if projectID == "" || variantID == "" || strings.TrimSpace(ref.StockID) == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	// 读当前清单走 model（join products 做工程隔离，与 ListVariantsByIDs 同一口径）；
	// 事务句柄透传，读到的就是调用方那一笔未提交的清单。
	var entity *productmodel.VariantEntity
	var err error
	if tx != nil {
		entity, err = s.m.GetVariantScopedTx(ctx, tx, variantID, projectID)
	} else {
		entity, err = s.m.GetVariantScoped(ctx, variantID, projectID)
	}
	if err != nil {
		return mapNotFound(err)
	}
	refs := productmodel.DecodeWarehouseSKUs(entity.WarehouseSKUs)
	next := productmodel.UpsertWarehouseSKURef(refs, productmodel.WarehouseSKURef{
		StockID:       ref.StockID,
		WarehouseID:   ref.WarehouseID,
		WarehouseCode: ref.WarehouseCode,
		SKUCode:       ref.SKUCode,
	})
	// tx 为 nil 时由 model 自己取句柄（product_variants 不在 RLS 名单，无需作用域）。
	return s.m.UpdateVariantWarehouseSKUsTx(ctx, tx, variantID, productmodel.EncodeWarehouseSKUs(next), time.Now().UTC())
}

// VariantSKUCodes 变体 → 它的 SKU 编码（契约见 VariantWarehouseStockPort）。
//
// 只读变体自己那一列，不碰库存表：调用方（库存）拿它当「第一次把货放进某个仓时，
// 那一行该叫什么码」的兜底，编码本体仍由仓库侧的归一规则决定（剥前缀 / 大写）。
func (s *Service) VariantSKUCodes(ctx context.Context, projectID string, variantIDs []string) (byVariant map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string]string, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	list, err := s.m.ListVariantsByIDs(ctx, variantIDs, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v == nil {
			continue
		}
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			out[v.ID] = code
		}
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
