// Package inventorycontract inventory 模块对外契约（issue #15）。
package inventorycontract

import (
	"context"

	inventorydto "go_wp/internal/module/inventory/dto"
)

// InventoryService 仓库与库存记录管理契约。
//
// 边界：本模块只管**库存真源**（inventory_stocks）与仓库实体（inventory_warehouses）。
//
//	· 商品模块只保留 product_variants.stock_total 这个**列表展示用冗余缓存**；
//	· 一切影响可用量的判断（扣减、超卖校验、可售数量）只能读本模块真源并加行锁，
//	  绝不读那个缓存 —— 缓存滞后会直接变成超卖；
//	· 采购 / 订单 / 客户各由自己的模块负责，本模块不反向依赖它们。
//
// 依赖方向：inventory → product（本模块实现 product 契约定义的变体库存端口，
// 由顶层装配注入商品模块），商品模块不认识仓库模块的实现。
type InventoryService interface {
	// —— 仓库（验收 1：可建仓库并设置一个默认仓）——
	// CreateWarehouse 建仓；工程内第一个仓自动成为默认仓（「必须有一个默认仓」），
	// 显式 IsDefault 则切换默认仓（同工程唯一）。
	CreateWarehouse(ctx context.Context, req *inventorydto.CreateWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	// UpdateWarehouse 改仓（改名 / 改短码 / 排序 / 状态 / 切换默认仓）。
	// 默认仓不能停用，也不能取消默认（先指定另一个默认仓）。
	UpdateWarehouse(ctx context.Context, req *inventorydto.UpdateWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	GetWarehouse(ctx context.Context, req *inventorydto.GetWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	// ListWarehouses 某工程的仓库列表（默认仓在最前）。
	ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error)
	// DeleteWarehouse 删仓：默认仓、仓内仍有非零库存时一律拒绝。
	DeleteWarehouse(ctx context.Context, req *inventorydto.DeleteWarehouseReq) (err error)

	// —— 库存记录（验收 2/3：SKU × 仓库 一行，新建变体自动生成初始 0 的记录）——
	// EnsureStock 幂等地确保某 SKU 在某仓有一条库存记录（初始 0）；
	// WarehouseID 为空时兜底到该工程的默认仓（未指定仓库时的兜底）。
	EnsureStock(ctx context.Context, req *inventorydto.EnsureStockReq) (res *inventorydto.StockResp, err error)
	// GetStock 单条库存记录（按 id，或按 变体 × 仓库）。
	GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error)
	// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4：同一 SKU 可在多个仓各有一行）。
	ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error)
	// ListStocks 库存记录列表（后台核对用，按工程 / 仓 / 商品 / 变体 / SKU 过滤 + 分页）。
	ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error)
}
