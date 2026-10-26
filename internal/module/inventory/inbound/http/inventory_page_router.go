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

}

// 三个整页的模板名（与路由 / 权限点同处：一眼能看全这个模块渲染了哪些屏）。
const (
	InventoryListPageTemplate       = "admin/inventory/inventory.html"
	InventoryWarehousesPageTemplate = "admin/inventory/inventory_warehouses.html"
	InventoryReasonsPageTemplate    = "admin/inventory/inventory_reasons.html"
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
	registerInventoryTransferPage(pages, svc, project, inventoryPages)
}
