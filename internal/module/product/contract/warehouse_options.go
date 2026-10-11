// warehouse_options.go — 商品后台页「仓库清单 + 每仓 SKU 候选」的只读端口。
//
// 消费者是商品模块自己的页面读模型（`service.ProductNewPage` 这类一屏一个读模型），
// 实现方是库存模块 —— 与 VariantAvailabilityPort / VariantStockPort 同一方向
// （本模块定义端口、对方实现、装配注入）。
//
// 注入链是**两跳**，改任一跳都会让「归属仓」静默消失（页面照常 200，无日志）：
// 装配 `SetInventoryDeps(warehousePort)` → `inbound/http/product_page.go` 转交
// `service.SetWarehouseOptionsPort` → 读模型字段。装配登记见
// `internal/routers/wiring.go` 的 `portProductPageWarehouses`。
//
// 为什么是**收窄端口**而不是直接把 inventorycontract.InventoryService 交给读模型：
// 那个接口上还有建仓 / 改仓 / 删仓 / 调库存这类写能力。渲染一个「商品新建页」不需要
// 其中任何一条，而接口一旦交给页面层，「顺手调一次库存」就在某次无关改动里变得可能。
// 越权防护靠接口形状，不靠调用方自觉。
//
// 返回的是**本模块的渲染视图**（dto 的重导出），不是库存模块的 dto：借对方的形状等于
// 本模块认识了对方的绑定层，对方改一个字段本模块跟着编译错（审计 CQ-004）。
package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// 跨模块形状：本模块页面的选项视图（dto 的重导出，不另造一组等价的定义）。
type (
	// WarehouseOptionView 仓库下拉 / 勾选项（含「默认仓」后缀的词条 key）。
	WarehouseOptionView = productdto.WarehouseOptionView
	// WarehouseSKUGroupView 一个仓的候选货集合。
	WarehouseSKUGroupView = productdto.WarehouseSKUGroupView
	// WarehouseSKUItemView 一个候选货（裸码 + 对方编码）。
	WarehouseSKUItemView = productdto.WarehouseSKUItemView
)

// WarehouseOptionsPort 仓库清单与每仓 SKU 候选的只读端口（由库存模块实现）。
//
// 未注入时（纯商品单测路径 / 未接库存契约）两个方法都返回空清单，页面照常渲染，
// 只是没有「归属仓」与「从仓库选」这两块 —— 与注入前 `h.inventories == nil` 的降级
// 逐字一致。
type WarehouseOptionsPort interface {
	// WarehouseOptions 某工程的仓库下拉项（默认仓在最前并标注）。
	WarehouseOptions(ctx context.Context, projectID string) (out []WarehouseOptionView, err error)
	// WarehouseSKUOptions 每仓的候选货（迁移 251 的「从仓库选」）。
	//
	// warehouses 由调用方传入而不是端口自己再查一次：调用方刚读过同一份仓库清单，
	// 让端口重查会把一次读取放大成两次往返。
	WarehouseSKUOptions(ctx context.Context, projectID string, warehouses []WarehouseOptionView) (out []WarehouseSKUGroupView, err error)
}
