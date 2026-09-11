// variant_stock.go — 变体归属仓与库存记录端口（issue #15）、库存缓存端口（issue #16）。
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

// VariantStockCachePort 商品侧库存**缓存**的读写端口（issue #16，由 product 模块实现）。
//
// 这是上一条依赖方向的反向那一半：库存变动由 inventory 发起，但
// product_variants.stock_total 是商品模块的表，跨模块写只能走商品模块自己的入口
// （表隔离约定）。两个端口方向相反、用途分明：
//
//	VariantStockPort      —— inventory 实现、product 调用（建变体时生成库存记录）；
//	VariantStockCachePort —— product 实现、inventory 调用（变动后同步展示缓存）。
//
// 三条不可动摇的约定：
//  1. 写缓存只更新 stock_total / stock_synced_at 两列（附同步时间戳），
//     绝不触碰价格、状态、SKU 等业务列；
//  2. 同步是库存事务**提交之后**的独立步骤 —— 跨模块写不得塞进同一个事务；
//  3. 缓存永远不参与可用量判断，对账以 inventory 真源为唯一依据。
type VariantStockCachePort interface {
	// SyncVariantStockTotal 把某个变体的缓存值写成 total 并盖上同步时间戳；
	// 变体不存在时返回错误（调用方按同步失败记账，不影响真源变动）。
	SyncVariantStockTotal(ctx context.Context, variantID string, total int) (err error)
	// ListVariantStockTotals 批量读缓存值（对账用；键为变体 id，不存在的 id 不出现）。
	ListVariantStockTotals(ctx context.Context, variantIDs []string) (totals map[string]int, err error)
}

// VariantAvailabilityPort SKU **可用量**的只读端口（issue #20，由 inventory 模块实现）。
//
// 与上面两个端口同源（inventory → product：本模块定义、inventory 实现、装配注入），
// 但用途是第三种：前两个管「建变体时生成记录」与「变动后同步缓存」，本端口管
// 「按真源读可用量」。捆绑品的数量上限与整单下限都要受可用量约束，而可用量的
// 唯一权威是 inventory_stocks —— 读 product_variants.stock_total 缓存会直接变成超卖
// （缓存滞后），这是 spec 的死线。
//
// 未注入时（纯商品单测路径）捆绑配置仍可保存与读取（配置本身不依赖库存），
// 但整单校验必须 fail-closed：拿不到权威可用量就返回错误，绝不按「无限制」放行。
type VariantAvailabilityPort interface {
	// AvailableQuantities 批量读 SKU 的可用量（键为变体 id；无库存记录的 id 值为 0）。
	// ProjectID 用于限定工程，避免跨工程读到同名变体。
	AvailableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error)
}
