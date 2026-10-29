// 商品仓库清单与 SKU 候选的只读端口，由库存适配器实现并在装配期注入。
// 数据只含资源事实，后端不生成下拉标签或选中态。
package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// 跨模块形状：本模块引用的仓库与 SKU 数据（dto 的重导出，不另造一组等价的定义）。
type (
	// WarehouseOptionResp 仓库身份、名称与默认标志。
	WarehouseOptionResp = productdto.WarehouseOptionResp
	// WarehouseSKUGroupResp 一个仓的候选货集合。
	WarehouseSKUGroupResp = productdto.WarehouseSKUGroupResp
	// WarehouseSKUItemResp 一个候选货（裸码 + 对方编码）。
	WarehouseSKUItemResp = productdto.WarehouseSKUItemResp
)

// WarehouseOptionsPort 仓库清单与每仓 SKU 候选的只读端口（由库存模块实现）。
//
// 未注入时（纯商品单测路径 / 未接库存契约）两个方法都返回空清单，页面照常渲染，
// 只是没有「归属仓」与「从仓库选」这两块 —— 与注入前 `h.inventories == nil` 的降级
// 逐字一致。
type WarehouseOptionsPort interface {
	// WarehouseOptions 某工程的仓库清单。
	WarehouseOptions(ctx context.Context, projectID string) (out []WarehouseOptionResp, err error)
	// WarehouseSKUOptions 每仓的候选货（迁移 251 的「从仓库选」）。
	//
	// warehouses 由调用方传入而不是端口自己再查一次：调用方刚读过同一份仓库清单，
	// 让端口重查会把一次读取放大成两次往返。
	WarehouseSKUOptions(ctx context.Context, projectID string, warehouses []WarehouseOptionResp) (out []WarehouseSKUGroupResp, err error)
}
