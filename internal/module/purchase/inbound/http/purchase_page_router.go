package purchasehttp

import (
	"github.com/gin-gonic/gin"

	adminui "go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
)

const (
	SourcesPageTemplate = "admin/purchase/sources.html"

	PurchasesPageTemplate = "admin/purchase/purchases.html"
)

var pageCatalogConsumers []variantCatalogConsumer

type variantCatalogConsumer interface {
	setVariantCatalog(catalog purchasecontract.VariantCatalog)
}

func SetVariantCatalog(catalog purchasecontract.VariantCatalog) {
	for _, consumer := range pageCatalogConsumers {
		consumer.setVariantCatalog(catalog)
	}
}

func SetupPurchasePages(pages *gin.RouterGroup, svc purchasecontract.PurchaseService,
	stock purchasecontract.StockPort, project projectcontract.ProjectService) {
	if pages == nil {
		return
	}

	sourcePages := NewSourcePageHandle(svc, project)
	purchases := NewPurchasePageHandle(svc, stock, project)
	pageCatalogConsumers = append(pageCatalogConsumers, purchases)

	pages.GET("/inventory/sources", adminui.PageAuthz("/api/inventory/source/list"),
		adminui.PageSlot(SourcesPageTemplate), sourcePages.SourcesPage)
	pages.POST("/inventory/sources/create", builtin.CasbinMiddlewareForPath("/api/inventory/source/create"), sourcePages.SourceCreate)
	pages.POST("/inventory/sources/update", builtin.CasbinMiddlewareForPath("/api/inventory/source/update"), sourcePages.SourceUpdate)
	pages.POST("/inventory/sources/delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.SourceDelete)
	pages.POST("/inventory/sources/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), sourcePages.SourcesBulkDelete)

	pages.GET("/inventory/purchases", adminui.PageAuthz("/api/inventory/purchase/list"),
		adminui.PageSlot(PurchasesPageTemplate), purchases.PurchasesPage)
	pages.POST("/inventory/purchases/create", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/create"), purchases.PurchaseCreate)
	pages.POST("/inventory/purchases/update", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseUpdate)
	pages.POST("/inventory/purchases/receipt", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/receipt"), purchases.PurchaseReceipt)
	pages.POST("/inventory/purchases/production", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/production"), purchases.PurchaseProduction)

	pages.POST("/inventory/purchases/finish", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseFinish)
	pages.POST("/inventory/purchases/cancel", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseCancel)

	pages.POST("/inventory/purchases/place", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchasePlace)
	pages.POST("/inventory/purchases/exception", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseException)
	pages.POST("/inventory/purchases/resume", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), purchases.PurchaseResume)
}
