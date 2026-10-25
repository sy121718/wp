package inventoryhttp

// inventory_page_router.go — 库存 / 仓库 / 变动原因 / 货源 / 采购入库页（/admin/inventory*）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 inventory_page_handle.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面写动作复用 API 的权限点 —— CasbinMiddlewareForPath 的路径是权限点声明的真源，
// 一个字符都不能改。页面 GET 的 Casbin 待补（见 docs/02-Z §4.3）。
//
// 商品契约（下拉数据源）留待装配收尾补注：见 SetProductCatalog 与 pageCatalogConsumers。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	inventorycontract "go_wp/internal/module/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// registerInventoryReadPages 库存域的只读整页（每一屏：路由 + 权限点 + 模板名）。
//
// 单独一个函数的理由与 product 同源：一屏一行、模板名就在路由旁边，
// 而不是在 handler 里再写一遍模板串 —— 失败与成功渲染的必须是同一张模板。
func registerInventoryReadPages(pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.GET("/inventory", adminui.PageAuthz("/api/inventory/warehouse/list"),
		adminui.PageSlot(InventoryListPageTemplate), h.InventoryPage)
	// 拆页（2026-09 评审第三轮）：仓库与原因字典各自独立成页 —— 原先它们与
	// 「手动改库存 / 看流水」挤在同一页靠 <details> 折叠，折叠不是解法。
	pages.GET("/inventory/warehouses", adminui.PageAuthz("/api/inventory/warehouse/list"),
		adminui.PageSlot(InventoryWarehousesPageTemplate), h.InventoryWarehousesPage)
	pages.GET("/inventory/reasons", adminui.PageAuthz("/api/inventory/reason/list"),
		adminui.PageSlot(InventoryReasonsPageTemplate), h.InventoryReasonsPage)

	// 仓库商品 SKU 台账页（迁移 624）：一行 = 一条库存行（含未认领行）。
	// 复用库存列表的权限点：能看库存的人就能看这张台账 —— 它只是同一个真源的另一个视角。
	pages.GET("/inventory/skus", adminui.PageAuthz("/api/inventory/stock/list"),
		adminui.PageSlot(InventorySKUsPageTemplate), h.InventorySKUsPage)
}

// 三个整页的模板名（与路由 / 权限点同处：一眼能看全这个模块渲染了哪些屏）。
const (
	InventoryListPageTemplate       = "admin/inventory/inventory.html"
	InventoryWarehousesPageTemplate = "admin/inventory/inventory_warehouses.html"
	InventoryReasonsPageTemplate    = "admin/inventory/inventory_reasons.html"
	InventorySourcesPageTemplate    = "admin/inventory/inventory_sources.html"
	InventoryPurchasesPageTemplate  = "admin/inventory/inventory_purchases.html"
	InventorySKUsPageTemplate       = "admin/inventory/inventory_skus.html"
	InventoryTransfersPageTemplate  = "admin/inventory/inventory_transfers.html"
)

// registerInventoryWarehouseWritePages 仓库与库存行的写端点（新建 / 编辑 / 删 / 批量删 /
// 库存调整 / 外码 / 跟踪开关 / 原因字典）。
//
// 全部复用 API 的权限点：CasbinMiddlewareForPath 的参数是权限点声明的真源，
// 一个字符都不能改（check-permission-gaps.sh 双向对账）。
func registerInventoryWarehouseWritePages(pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), h.InventoryWarehouseCreate)
	pages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), h.InventoryWarehouseUpdate)
	pages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), h.InventoryWarehouseDefault)
	pages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), h.InventoryWarehouseDelete)
	// 批量删除复用单条删除的权限点：批量不是新能力，只是把 N 次单条动作压成一次提交。
	pages.POST("/inventory/warehouses/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), h.InventoryWarehousesBulkDelete)
	// 库存变动（走真源行锁 + 流水）与三个行内编辑入口：全部复用 stock/change 的权限点 ——
	// 登记外码 / 切跟踪开关 / 改数量都属 inventory_stocks 的写权限
	//（能改这个仓库存的人，才该能改它的外码映射与跟踪开关）。
	pages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockChange)
	pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryExternalSKUUpdate)
	pages.POST("/inventory/stock/tracking", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockTrackingUpdate)
	// 原因字典：启停 / 改名（内置原因改名由 service 拒绝，停用照常）。
	pages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), h.InventoryReasonCreate)
	pages.POST("/inventory/reason/update", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), h.InventoryReasonUpdate)
	pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), h.InventoryReasonsBulkStatus)
}

// registerInventorySKUWritePages 仓库商品 SKU 台账页的三个写动作（迁移 624）：
// 认领 / 解除认领 / 新建未认领存货。
//
// **全部复用** /api/inventory/stock/change —— 与外部编码、跟踪开关同口径：
// 能改这个仓库存的人，才该能认领它 / 给它建档。不新增权限点，也不新增 authorizedAPI 路由。
// 三条都写成**字面量**的 CasbinMiddlewareForPath 调用而不是先存进变量：
// scripts/check-page-endpoint-authz.py 按调用形状判授权（`args 里出现 CasbinMiddleware`），
// 抽成变量会让门禁看不见鉴权、把三条全判成「登录即可用」。
func registerInventorySKUWritePages(pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.POST("/inventory/stock/claim", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockClaim)
	pages.POST("/inventory/stock/unclaim", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockUnclaim)
	pages.POST("/inventory/stock/unclaimed-create", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockUnclaimedCreate)
}

// registerInventoryTransferPage 调拨单页（迁移 630，W3）：列表整页；
// 建单 / 出货 / 签收 / 取消走 JSON API（/api/inventory/transfer/*）——
// 页面不另挂写端点（同一份授权不写两处）。仓库下拉复用库存页那份口径。
func registerInventoryTransferPage(pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService, h *inventoryPageHandle) {
	transferPages := NewInventoryTransferPageHandle(svc, project, h.listWarehouses)
	pages.GET("/inventory/transfers", adminui.PageAuthz("/api/inventory/transfer/list"),
		adminui.PageSlot(InventoryTransfersPageTemplate), transferPages.InventoryTransfersPage)
}

// SetupInventoryPages 注册库存域后台页面；pages 为 nil 时整体跳过。
func SetupInventoryPages(pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	// 商品契约（下拉数据源）留待装配收尾补注，见 SetProductCatalog。
	inventoryPages := NewInventoryPageHandle(svc, project, nil)
	pageCatalogConsumers = append(pageCatalogConsumers, inventoryPages)
	registerInventoryReadPages(pages, inventoryPages)
	registerInventoryWarehouseWritePages(pages, inventoryPages)
	registerInventorySKUWritePages(pages, inventoryPages)
	registerInventoryTransferPage(pages, svc, project, inventoryPages)

	// 货源管理页（issue #17）：类型与关联方两个结构化维度支撑报表区分。
	sourcePages := NewInventorySourcePageHandle(svc, project)
	pages.GET("/inventory/sources", adminui.PageAuthz("/api/inventory/source/list"),
		adminui.PageSlot(InventorySourcesPageTemplate), sourcePages.InventorySourcesPage)
	pages.POST("/inventory/sources/create", builtin.CasbinMiddlewareForPath("/api/inventory/source/create"), sourcePages.InventorySourceCreate)
	pages.POST("/inventory/sources/update", builtin.CasbinMiddlewareForPath("/api/inventory/source/update"), sourcePages.InventorySourceUpdate)
	pages.POST("/inventory/sources/delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.InventorySourceDelete)
	pages.POST("/inventory/sources/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.InventorySourcesBulkDelete)

	// 采购入库页（issue #18）：采购单 → 逐行收货入库（复用 #16 的变动契约）+ 生产入库 + 进货历史。
	purchasePages := NewInventoryPurchasePageHandle(svc, project, nil)
	pageCatalogConsumers = append(pageCatalogConsumers, purchasePages)
	pages.GET("/inventory/purchases", adminui.PageAuthz("/api/inventory/purchase/list"),
		adminui.PageSlot(InventoryPurchasesPageTemplate), purchasePages.InventoryPurchasesPage)
	pages.POST("/inventory/purchases/create", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/create"), purchasePages.InventoryPurchaseCreate)
	pages.POST("/inventory/purchases/receipt", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/receipt"), purchasePages.InventoryPurchaseReceipt)
	pages.POST("/inventory/purchases/production", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/production"), purchasePages.InventoryPurchaseProduction)
	// 采购单终态（迁移 626, W2）：提前关单 / 取消。权限点复用采购单更新 ——
	// 关单是一次对采购单的改写，不是新能力，不该多一个权限点。
	pages.POST("/inventory/purchases/finish", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchasePages.InventoryPurchaseFinish)
	pages.POST("/inventory/purchases/cancel", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchasePages.InventoryPurchaseCancel)
	// 生产入库（自家工厂）属于「有单据来源的手工入库」，表单留在采购入库页：
	// 它的页面 action 是 /admin/inventory/purchases/production，权限点仍是
	// api/inventory/purchase/production（权限点是声明真源，不随页面走）。
	// 库存管理页只保留「库存调整（盘点 / 报损）」这一个写入口，因此这里不再暴露
	// /admin/inventory/production —— 留一个没有表单指向的页面路由只会变成第二个入口。
}
