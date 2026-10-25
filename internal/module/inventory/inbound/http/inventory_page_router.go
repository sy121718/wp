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
	// 仓库商品 SKU 台账页（迁移 621）：一行 = 一条库存行（含未认领行）。
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
)

// registerInventorySKUWritePages 仓库商品 SKU 台账页的三个写动作（迁移 621）：
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
	pages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), inventoryPages.InventoryWarehouseCreate)
	pages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseUpdate)
	pages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseDefault)
	pages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), inventoryPages.InventoryWarehouseDelete)
	// 批量删除复用单条删除的权限点：批量不是新能力，只是把 N 次单条动作压成一次提交。
	pages.POST("/inventory/warehouses/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), inventoryPages.InventoryWarehousesBulkDelete)
	// 库存变动与原因字典（issue #16）：变动走真源行锁 + 流水，原因新建走原因字典。
	pages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryStockChange)
	// 库存页行内编辑外部编码（迁移 251 的可编辑入口）：**复用**本页唯一写入口的权限点，
	// 不新增权限点、不新增 authorizedAPI 路由 —— 登记外码不是新能力，而是库存行属性的就地维护，
	// 与「库存调整」同属 inventory_stocks 的写权限（能改这个仓库存的人，才该能改它的外码映射）。
	// CasbinMiddlewareForPath 的参数是权限点声明的真源，一个字符都不能改。
	pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryExternalSKUUpdate)
	// 库存页行内编辑「跟踪开关 + 数量」（迁移 261）：同样**复用**本页唯一写入口的权限点，
	// 不新增权限点、不新增 authorizedAPI 路由 —— 切开关与改数量都落在同一条
	// inventory_stocks 写权限上（能改这个仓库存的人，才该能改它跟不跟踪）。
	// 数量本身仍走变动契约（手工调整）并写流水，行内表单只是入口。
	pages.POST("/inventory/stock/tracking", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), inventoryPages.InventoryStockTrackingUpdate)
	registerInventorySKUWritePages(pages, inventoryPages)
	pages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), inventoryPages.InventoryReasonCreate)
	// 启停 / 改名：内置原因改名由 service 拒绝（它的 key 由系统按 code 派生），停用照常。
	pages.POST("/inventory/reason/update", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), inventoryPages.InventoryReasonUpdate)
	pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), inventoryPages.InventoryReasonsBulkStatus)

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
	// 采购单终态（迁移 623, W2）：提前关单 / 取消。权限点复用采购单更新 ——
	// 关单是一次对采购单的改写，不是新能力，不该多一个权限点。
	pages.POST("/inventory/purchases/finish", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchasePages.InventoryPurchaseFinish)
	pages.POST("/inventory/purchases/cancel", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchasePages.InventoryPurchaseCancel)
	// 生产入库（自家工厂）属于「有单据来源的手工入库」，表单留在采购入库页：
	// 它的页面 action 是 /admin/inventory/purchases/production，权限点仍是
	// api/inventory/purchase/production（权限点是声明真源，不随页面走）。
	// 库存管理页只保留「库存调整（盘点 / 报损）」这一个写入口，因此这里不再暴露
	// /admin/inventory/production —— 留一个没有表单指向的页面路由只会变成第二个入口。
}
