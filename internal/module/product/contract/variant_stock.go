// variant_stock.go — 变体归属仓与库存只读端口（issue #15 / #20 / #24；#16 缓存端口已随 121 删除）。
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
// 库存记录是**真源**（inventory_stocks）；121 起商品表无 stock_total 列，
// 展示值走查询期投影，商品模块自身不做任何可用量判断。

// VariantAvailabilityPort SKU **可用量**的只读端口（issue #20，由 inventory 模块实现）。
//
// 与上面两个端口同源（inventory → product：本模块定义、inventory 实现、装配注入），
// 本端口管「按真源读可用量」。捆绑品的数量上限与整单下限都要受可用量约束，
// 唯一权威是 inventory_stocks（带行锁），展示投影不参与扣减。
//
// 未注入时（纯商品单测路径）捆绑配置仍可保存与读取（配置本身不依赖库存），
// 但整单校验必须 fail-closed：拿不到权威可用量就返回错误，绝不按「无限制」放行。
type VariantAvailabilityPort interface {
	// AvailableQuantities 批量读 SKU 的可用量（键为变体 id；无库存记录的 id 值为 0）。
	// ProjectID 用于限定工程，避免跨工程读到同名变体。
	AvailableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error)
}

// VariantAvailabilityLookupPort 按**变体 id** 查可用量的只读端口（issue #24，由 product 模块实现）。
//
// 与上面 VariantAvailabilityPort 的分工：那个是「product 调 inventory」的底层端口（要 ProjectID，
// 由 inventory 实现）；本端口是它面向**访问面**的包装 —— 静态产物里烘的只有变体 id（data 属性），
// 片段端点没有工程上下文，工程由 product 模块按变体反查补齐。调用方因此既不用认识商品表结构，
// 也不用伪造工程 id。
//
// 用途：商品详情规格选择器旁的「可用量」片段（runtimefragment 的 productVariantAvailability）——
// 库存是运行期真源，构建期不可能把可用量烘进静态产物，只能每次请求现读（docs/04 §1.1）。
type VariantAvailabilityLookupPort interface {
	// VariantAvailabilities 批量读可用量：键为变体 id，未知 / 已删除的 id 不出现在结果里。
	// 端口未注入（inventory 未装配）时返回空结果而不报错 —— 调用方据此渲染「以结算为准」的降级文案；
	// 缺货判定与加购校验另在写路径上做，不靠本端口兜底。
	VariantAvailabilities(ctx context.Context, variantIDs []string) (map[string]int, error)
}
