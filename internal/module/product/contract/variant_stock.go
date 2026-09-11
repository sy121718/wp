// variant_stock.go — 变体归属仓与库存记录端口（issue #15）。
//
// 依赖方向是 inventory → product：本模块只**定义端口**（不认识仓库模块的实现），
// 由 inventory 模块实现、顶层装配注入。未注入时（纯商品单测路径）：
//
//	· 变体创建不生成库存记录；
//	· SKU 编码退回「商品 slug + 随机段」，不带仓库短码前缀。
//
// 生产装配恒注入（routers.SetupRoutes），因此线上 SKU 一律带归属仓短码前缀。
package productcontract

import "context"

// WarehouseRef 归属仓的只读引用。
//
// Code 是仓库短码，参与 SKU 编码（{仓短码}_{商品码}_{序号}）；
// ProjectID 回填给商品模块用于生成库存记录，避免商品侧自己猜工程。
type WarehouseRef struct {
	ID        string
	ProjectID string
	Code      string
	Name      string
}

// VariantStockPort 变体归属仓与库存记录端口（由 inventory 模块实现）。
//
// 商品模块建变体时需要两件仓库模块才知道的事：
//  1. ResolveWarehouse：解析归属仓（warehouseID 为空 → 该工程的默认仓）并给出短码；
//  2. EnsureVariantStock：在归属仓为该 SKU 生成一条初始 0 的库存记录（幂等）。
//
// 库存记录是**真源**（inventory_stocks）；商品侧的 product_variants.stock_total
// 只是列表展示用缓存，商品模块自身不做任何可用量判断。
type VariantStockPort interface {
	ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *WarehouseRef, err error)
	EnsureVariantStock(ctx context.Context, ref *WarehouseRef, productID, variantID, skuCode string) (err error)
}
