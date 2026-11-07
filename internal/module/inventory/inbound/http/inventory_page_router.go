package inventoryhttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	inventorycontract "go_wp/internal/module/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

func registerInventoryReadPages(pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.GET("/inventory", mw.PageAuthz("/api/inventory/warehouse/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryPage)

	pages.GET("/inventory/warehouses", mw.PageAuthz("/api/inventory/warehouse/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryWarehousesPage)
	pages.GET("/inventory/reasons", mw.PageAuthz("/api/inventory/reason/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryReasonsPage)

	pages.GET("/inventory/skus", mw.PageAuthz("/api/inventory/stock/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryStockSKUsPage)

	pages.GET("/inventory/movements", mw.PageAuthz("/api/inventory/movement/list"),
		pageview.UseRenderer(pageview.RenderPage), h.InventoryMovementsPage)
}

const (
	InventoryListPageTemplate       = "admin/inventory/inventory.html"
	InventoryWarehousesPageTemplate = "admin/inventory/inventory_warehouses.html"
	InventoryReasonsPageTemplate    = "admin/inventory/inventory_reasons.html"
	InventoryTransfersPageTemplate  = "admin/inventory/inventory_transfers.html"
	InventoryStockSKUsPageTemplate  = "admin/inventory/inventory_stock_skus.html"
	InventoryMovementsPageTemplate  = "admin/inventory/inventory_movements.html"
)

func registerInventoryWarehouseWritePages(pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), pageview.HTMLFragment, h.InventoryWarehouseCreate)
	pages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), pageview.HTMLFragment, h.InventoryWarehouseUpdate)
	pages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), pageview.HTMLFragment, h.InventoryWarehouseDefault)
	pages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), pageview.HTMLFragment, h.InventoryWarehouseDelete)

	pages.POST("/inventory/warehouses/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), pageview.HTMLFragment, h.InventoryWarehousesBulkDelete)

	pages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockChange)
	pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryExternalSKUUpdate)
	pages.POST("/inventory/stock/tracking", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockTrackingUpdate)

	pages.POST("/inventory/stock/create", builtin.CasbinMiddlewareForPath("/api/inventory/stock/ensure"), pageview.HTMLFragment, h.InventoryStockCreate)
	pages.POST("/inventory/stock/update", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockUpdate)
	pages.POST("/inventory/stock/status", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), pageview.HTMLFragment, h.InventoryStockStatus)

	pages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), pageview.HTMLFragment, h.InventoryReasonCreate)
	pages.POST("/inventory/reason/update", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), pageview.HTMLFragment, h.InventoryReasonUpdate)
	pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), pageview.HTMLFragment, h.InventoryReasonsBulkStatus)
}

func registerInventoryTransferPage(pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService, h *inventoryPageHandle) {
	transferPages := NewInventoryTransferPageHandle(svc, project, h.listWarehouses)
	pages.GET("/inventory/transfers", mw.PageAuthz("/api/inventory/transfer/list"),
		pageview.UseRenderer(pageview.RenderPage), transferPages.InventoryTransfersPage)
}

func SetupInventoryPages(pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService) {
	if pages == nil {
		return
	}

	inventoryPages := NewInventoryPageHandle(svc, project, nil)
	pageCatalogConsumers = append(pageCatalogConsumers, inventoryPages)
	registerInventoryReadPages(pages, inventoryPages)
	registerInventoryWarehouseWritePages(pages, inventoryPages)
	registerInventoryTransferPage(pages, svc, project, inventoryPages)
}
