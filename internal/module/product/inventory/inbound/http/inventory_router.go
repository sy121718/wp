// inventory_router.go — inventory 模块路由自装配（issue #15 / #16）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。
package inventoryhttp

import (
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	inventoryservice "go_wp/internal/module/product/inventory/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupInventoryRoutes 装配 inventory 模块路由，返回模块契约。
// project 用于把「未指定工程」解析为唯一工程（warehouses.project_id 为 NOT NULL 外键）。
//
// 返回的契约同时实现了 product 契约定义的 VariantStockPort（ResolveWarehouse /
// EnsureVariantStock）—— 顶层装配时注入商品模块（依赖方向 inventory → product）。
func SetupInventoryRoutes(rg *permission.RouteGroup, db *gorm.DB, project projectcontract.ProjectService) inventorycontract.InventoryService {
	svc := inventoryservice.NewService(inventorymodel.NewModel(db), project)
	handle := NewHandle(svc)

	g := rg.Group("/inventory")
	// 仓库（验收 1）：工程内第一个仓自动成为默认仓，显式 isDefault 即切换默认仓。
	g.GET("/warehouse/list", permission.InventoryWarehouseList, handle.ListWarehouses)
	g.GET("/warehouse/get", permission.InventoryWarehouseGet, handle.GetWarehouse)
	g.POST("/warehouse/create", permission.InventoryWarehouseCreate, handle.CreateWarehouse)
	g.POST("/warehouse/update", permission.InventoryWarehouseUpdate, handle.UpdateWarehouse)
	g.POST("/warehouse/delete", permission.InventoryWarehouseDelete, handle.DeleteWarehouse)
	// 库存记录（验收 2/3/4）：ensure 是幂等的「确保某 SKU 在某仓有一行」，
	// list 支持按 SKU 查各仓库存（维度是 SKU × 仓库）。
	g.GET("/stock/list", permission.InventoryStockList, handle.ListStocks)
	g.GET("/stock/sku", permission.InventoryStockSKU, handle.ListStocksBySKU)
	g.GET("/stock/get", permission.InventoryStockGet, handle.GetStock)
	g.POST("/stock/ensure", permission.InventoryStockEnsure, handle.EnsureStock)

	// 库存变动与流水（issue #16 验收 1–5）：真源行锁增减 + 流水 + 原因字典 + 物料清单。
	g.POST("/stock/change", permission.InventoryStockChange, handle.ChangeStock)
	g.POST("/stock/deduct", permission.InventoryStockDeduct, handle.DeductStock)
	g.GET("/movement/list", permission.InventoryMovementList, handle.ListMovements)
	g.GET("/reason/list", permission.InventoryReasonList, handle.ListReasons)
	g.POST("/reason/create", permission.InventoryReasonCreate, handle.CreateReason)
	g.POST("/reason/update", permission.InventoryReasonUpdate, handle.UpdateReason)
	g.POST("/bom/set", permission.InventoryBomSet, handle.SetBOM)
	g.GET("/bom/get", permission.InventoryBomGet, handle.GetBOM)

	// 货源（issue #17）：一张表承载全部进货来源 —— 外部供应商 / 集团内关联公司 / 自家工厂
	// 用类型区分，关联方标志独立成列（报表区分），config 承载异构对接扩展信息。
	// list 的 type / relatedParty / status / keyword 都是可组合的报表筛选维度，
	// summary 按「类型 × 关联方」分组给出交叉计数。
	g.GET("/source/list", permission.InventorySourceList, handle.ListSources)
	g.GET("/source/get", permission.InventorySourceGet, handle.GetSource)
	g.GET("/source/summary", permission.InventorySourceSummary, handle.SourceSummary)
	g.POST("/source/create", permission.InventorySourceCreate, handle.CreateSource)
	g.POST("/source/update", permission.InventorySourceUpdate, handle.UpdateSource)
	g.POST("/source/delete", permission.InventorySourceDelete, handle.DeleteSource)

	// 采购单与入库（issue #18）：采购单（来源 = #17 的货源）→ 收货入库（复用 #16 的 ChangeStock）。
	// receipt 支持按行分批累加已入库数量，带幂等键防重复入库；production 是自家工厂
	// 生产入库（无采购单，成本价手工填写）；history 是某 SKU 的进货历史。
	g.GET("/purchase/list", permission.InventoryPurchaseList, handle.ListPurchaseOrders)
	g.GET("/purchase/get", permission.InventoryPurchaseGet, handle.GetPurchaseOrder)
	g.GET("/purchase/history", permission.InventoryPurchaseHistory, handle.ListPurchaseHistory)
	g.POST("/purchase/create", permission.InventoryPurchaseCreate, handle.CreatePurchaseOrder)
	g.POST("/purchase/update", permission.InventoryPurchaseUpdate, handle.UpdatePurchaseOrder)
	g.POST("/purchase/receipt", permission.InventoryPurchaseReceipt, handle.RegisterReceipt)
	g.POST("/purchase/production", permission.InventoryPurchaseProduction, handle.RegisterProductionInbound)

	// 商品侧缓存同步与对账（issue #16 验收 6/7）：提交后的独立步骤，不进变动事务。
	return svc
}
