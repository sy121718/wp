package inventoryhttp

import (
	"github.com/gin-gonic/gin"
	"net/http"

	"go_wp/internal/middleware/builtin"
	inventorycontract "go_wp/internal/module/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

func registerInventoryReadPages(reg *mw.Registry, pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.GET("/inventory", mw.PageAuthz("/api/inventory/warehouse/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory", "/api/inventory/warehouse/list")

	pages.GET("/inventory/warehouses", mw.PageAuthz("/api/inventory/warehouse/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryWarehousesPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/warehouses", "/api/inventory/warehouse/list")
	pages.GET("/inventory/reasons", mw.PageAuthz("/api/inventory/reason/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryReasonsPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/reasons", "/api/inventory/reason/list")

	pages.GET("/inventory/skus", mw.PageAuthz("/api/inventory/stock/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryStockSKUsPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/skus", "/api/inventory/stock/list")

	pages.GET("/inventory/movements", mw.PageAuthz("/api/inventory/movement/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryMovementsPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/movements", "/api/inventory/movement/list")
}

const (
	InventoryListPageTemplate       = "admin/inventory/inventory.html"
	InventoryWarehousesPageTemplate = "admin/inventory/inventory_warehouses.html"
	InventoryReasonsPageTemplate    = "admin/inventory/inventory_reasons.html"
	InventoryTransfersPageTemplate  = "admin/inventory/inventory_transfers.html"
	InventoryStockSKUsPageTemplate  = "admin/inventory/inventory_stock_skus.html"
	InventoryMovementsPageTemplate  = "admin/inventory/inventory_movements.html"
)

func registerInventoryWarehouseWritePages(reg *mw.Registry, pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), pageview.HTMLFragment, h.InventoryWarehouseCreate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/warehouse/create", "/api/inventory/warehouse/create", http.MethodPost)
	pages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), pageview.HTMLFragment, h.InventoryWarehouseUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/warehouse/update", "/api/inventory/warehouse/update", http.MethodPost)
	pages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), pageview.HTMLFragment, h.InventoryWarehouseDefault)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/warehouse/default", "/api/inventory/warehouse/update", http.MethodPost)
	pages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), pageview.HTMLFragment, h.InventoryWarehouseDelete)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/warehouse/delete", "/api/inventory/warehouse/delete", http.MethodPost)
	pages.POST("/inventory/warehouses/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), pageview.HTMLFragment, h.InventoryWarehousesBulkDelete)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/warehouses/bulk-delete", "/api/inventory/warehouse/delete", http.MethodPost)
	pages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockChange)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/stock/change", "/api/inventory/stock/change", http.MethodPost)
	pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryExternalSKUUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/external-sku", "/api/inventory/stock/change", http.MethodPost)
	pages.POST("/inventory/stock/tracking", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockTrackingUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/stock/tracking", "/api/inventory/stock/change", http.MethodPost)
	pages.POST("/inventory/stock/create", builtin.CasbinMiddlewareForPath("/api/inventory/stock/ensure"), pageview.HTMLFragment, h.InventoryStockCreate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/stock/create", "/api/inventory/stock/ensure", http.MethodPost)
	pages.POST("/inventory/stock/update", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/stock/update", "/api/inventory/stock/change", http.MethodPost)
	pages.POST("/inventory/stock/status", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockStatus)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/stock/status", "/api/inventory/stock/change", http.MethodPost)
	pages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), pageview.HTMLFragment, h.InventoryReasonCreate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/reason/create", "/api/inventory/reason/create", http.MethodPost)
	pages.POST("/inventory/reason/update", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), pageview.HTMLFragment, h.InventoryReasonUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/reason/update", "/api/inventory/reason/update", http.MethodPost)
	pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), pageview.HTMLFragment, h.InventoryReasonsBulkStatus)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/reasons/bulk-status", "/api/inventory/reason/update", http.MethodPost)
}

func registerInventoryTransferPage(reg *mw.Registry, pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService, h *inventoryPageHandle) {
	transferPages := NewInventoryTransferPageHandle(svc, project, h.listWarehouses)
	pages.GET("/inventory/transfers", mw.PageAuthz("/api/inventory/transfer/list"),
		pageview.UseRenderer(pageview.RenderPage), transferPages.InventoryTransfersPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/transfers", "/api/inventory/transfer/list")
}

func SetupInventoryPages(reg *mw.Registry, pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService) {
	if pages == nil {
		return
	}

	inventoryPages := NewInventoryPageHandle(svc, project, nil)
	pageCatalogConsumers = append(pageCatalogConsumers, inventoryPages)
	registerInventoryReadPages(reg, pages, inventoryPages)
	registerInventoryWarehouseWritePages(reg, pages, inventoryPages)
	registerInventoryTransferPage(reg, pages, svc, project, inventoryPages)
}
