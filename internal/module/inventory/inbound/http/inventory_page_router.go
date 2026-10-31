package inventoryhttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	inventorycontract "go_wp/internal/module/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
	pageview "go_wp/pkg/view"
)

func registerInventoryReadPages(pages *gin.RouterGroup, h *inventoryPageHandle) {
	pages.GET("/inventory", adminui.PageAuthz("/api/inventory/warehouse/list"),
		pageview.UseRenderer(adminui.RenderPage), h.InventoryPage)

	pages.GET("/inventory/warehouses", adminui.PageAuthz("/api/inventory/warehouse/list"),
		pageview.UseRenderer(adminui.RenderPage), h.InventoryWarehousesPage)
	pages.GET("/inventory/reasons", adminui.PageAuthz("/api/inventory/reason/list"),
		pageview.UseRenderer(adminui.RenderPage), h.InventoryReasonsPage)

	pages.GET("/inventory/skus", adminui.PageAuthz("/api/inventory/stock/list"),
		pageview.UseRenderer(adminui.RenderPage), h.InventoryStockSKUsPage)

	pages.GET("/inventory/movements", adminui.PageAuthz("/api/inventory/movement/list"),
		pageview.UseRenderer(adminui.RenderPage), h.InventoryMovementsPage)
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
	pages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), h.InventoryWarehouseCreate)
	pages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), h.InventoryWarehouseUpdate)
	pages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), h.InventoryWarehouseDefault)
	pages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), h.InventoryWarehouseDelete)

	pages.POST("/inventory/warehouses/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), h.InventoryWarehousesBulkDelete)

	pages.POST("/inventory/stock/change", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockChange)
	pages.POST("/inventory/external-sku", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryExternalSKUUpdate)
	pages.POST("/inventory/stock/tracking", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockTrackingUpdate)

	pages.POST("/inventory/stock/create", builtin.CasbinMiddlewareForPath("/api/inventory/stock/ensure"), h.InventoryStockCreate)
	pages.POST("/inventory/stock/update", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockUpdate)
	pages.POST("/inventory/stock/status", builtin.CasbinMiddlewareForPath("/api/inventory/stock/change"), h.InventoryStockStatus)

	pages.POST("/inventory/reason/create", builtin.CasbinMiddlewareForPath("/api/inventory/reason/create"), h.InventoryReasonCreate)
	pages.POST("/inventory/reason/update", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), h.InventoryReasonUpdate)
	pages.POST("/inventory/reasons/bulk-status", builtin.CasbinMiddlewareForPath("/api/inventory/reason/update"), h.InventoryReasonsBulkStatus)
}

func registerInventoryTransferPage(pages *gin.RouterGroup, svc inventorycontract.InventoryService,
	project projectcontract.ProjectService, h *inventoryPageHandle) {
	transferPages := NewInventoryTransferPageHandle(svc, project, h.listWarehouses)
	pages.GET("/inventory/transfers", adminui.PageAuthz("/api/inventory/transfer/list"),
		pageview.UseRenderer(adminui.RenderPage), transferPages.InventoryTransfersPage)
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
