package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_inventory.go - 库存与主数据页路由：仓库/变动、货源、采购入库、变更记录。

func setupInventoryRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 库存管理页（issue #15）：仓库实体（短码 / 名称 / 状态 / 默认仓）与「某 SKU 的各仓库存」。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用仓库 API 权限点。
	// 库存读的是仓库模块真源，与商品页的「库存缓存」列是两回事。
	inventoryPages := NewInventoryPageHandle(d.inventories, d.projects, d.products)
	adminPages.GET("/inventory", inventoryPages.InventoryPage)
	adminPages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), inventoryPages.InventoryWarehouseCreate)
	adminPages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseUpdate)
	adminPages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseDefault)
	adminPages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), inventoryPages.InventoryWarehouseDelete)
	// 库存变动与原因字典（issue #16）：表单写动作复用对应 API 权限点。
	// 变动走 /api/inventory/stock/change（真源行锁 + 流水），原因新建走 /api/inventory/reason/create。
	adminPages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryStockChange)
	adminPages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), inventoryPages.InventoryReasonCreate)

	// 货源管理页（issue #17）：外部供应商 / 集团内关联公司 / 自家工厂登记在同一张表，
	// 类型与关联方标志两个结构化维度支撑报表区分，对接配置（JSON 对象）承载异构扩展信息。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用货源 API 权限点。
	sourcePages := NewInventorySourcePageHandle(d.inventories, d.projects)
	adminPages.GET("/inventory/sources", sourcePages.InventorySourcesPage)
	adminPages.POST("/inventory/sources/create", builtin.CasbinMiddlewareForPath("/api/inventory/source/create"), sourcePages.InventorySourceCreate)
	adminPages.POST("/inventory/sources/update", builtin.CasbinMiddlewareForPath("/api/inventory/source/update"), sourcePages.InventorySourceUpdate)
	adminPages.POST("/inventory/sources/delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.InventorySourceDelete)

	// 采购入库页（issue #18）：采购单（来源 = #17 的货源）→ 逐行登记收货入库
	//（复用 #16 的变动契约，库存真源 + 流水 + 成本价一并落地）+ 自家工厂生产入库 + 进货历史。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用采购 API 权限点。
	purchasePages := NewInventoryPurchasePageHandle(d.inventories, d.projects, d.products)
	adminPages.GET("/inventory/purchases", purchasePages.InventoryPurchasesPage)
	adminPages.POST("/inventory/purchases/create", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/create"), purchasePages.InventoryPurchaseCreate)
	adminPages.POST("/inventory/purchases/receipt", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/receipt"), purchasePages.InventoryPurchaseReceipt)
	adminPages.POST("/inventory/purchases/production", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/production"), purchasePages.InventoryPurchaseProduction)

	// 变更记录页（issue #19）：主数据（商品 / 变体 / 货源）的字段级变更历史。
	// 只读页面：没有写表单 —— 记录由业务模块在写操作里经 masterdata 契约追加，
	// 后台不提供「手工补一条」的口子。按实体查询（实体清单点一行即锁定该实体）。
	masterDataPages := NewMasterDataChangePageHandle(d.masterdata, d.projects)
	adminPages.GET("/masterdata/changes", masterDataPages.MasterDataChangesPage)
}
