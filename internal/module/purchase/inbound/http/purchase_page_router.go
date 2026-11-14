package purchasehttp

import (
	"github.com/gin-gonic/gin"
	"net/http"

	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
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

func SetupPurchasePages(reg *mw.Registry, pages *gin.RouterGroup, svc purchasecontract.PurchaseService,
	stock purchasecontract.StockPort, project projectcontract.ProjectService) {
	if pages == nil {
		return
	}

	sourcePages := NewSourcePageHandle(svc, project)
	purchases := NewPurchasePageHandle(svc, stock, project)
	pageCatalogConsumers = append(pageCatalogConsumers, purchases)

	pages.GET("/inventory/sources", mw.PageAuthz("/api/inventory/source/list"),
		pageview.Slot(SourcesPageTemplate, pageview.RenderPage), sourcePages.SourcesPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/sources", "/api/inventory/source/list")
	pages.POST("/inventory/sources/create", builtin.CasbinMiddlewareForPath("/api/inventory/source/create"), pageview.HTMLFragment, sourcePages.SourceCreate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/sources/create", "/api/inventory/source/create", http.MethodPost)
	pages.POST("/inventory/sources/update", builtin.CasbinMiddlewareForPath("/api/inventory/source/update"), pageview.HTMLFragment, sourcePages.SourceUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/sources/update", "/api/inventory/source/update", http.MethodPost)
	pages.POST("/inventory/sources/delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), pageview.HTMLFragment, sourcePages.SourceDelete)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/sources/delete", "/api/inventory/source/delete", http.MethodPost)
	pages.POST("/inventory/sources/bulk-delete", builtin.CasbinMiddlewareForPath("/api/inventory/source/delete"), pageview.HTMLFragment, sourcePages.SourcesBulkDelete)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/sources/bulk-delete", "/api/inventory/source/delete", http.MethodPost)

	pages.GET("/inventory/purchases", mw.PageAuthz("/api/inventory/purchase/list"),
		pageview.Slot(PurchasesPageTemplate, pageview.RenderPage), purchases.PurchasesPage)
	mw.DeclarePageGetOn(reg, "/admin/inventory/purchases", "/api/inventory/purchase/list")
	pages.POST("/inventory/purchases/create", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/create"), pageview.HTMLFragment, purchases.PurchaseCreate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/create", "/api/inventory/purchase/create", http.MethodPost)
	pages.POST("/inventory/purchases/update", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), pageview.HTMLFragment, purchases.PurchaseUpdate)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/update", "/api/inventory/purchase/update", http.MethodPost)
	pages.POST("/inventory/purchases/receipt", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/receipt"), pageview.HTMLFragment, purchases.PurchaseReceipt)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/receipt", "/api/inventory/purchase/receipt", http.MethodPost)
	pages.POST("/inventory/purchases/production", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/production"), pageview.HTMLFragment, purchases.PurchaseProduction)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/production", "/api/inventory/purchase/production", http.MethodPost)

	pages.POST("/inventory/purchases/finish", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), pageview.HTMLFragment, purchases.PurchaseFinish)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/finish", "/api/inventory/purchase/update", http.MethodPost)
	pages.POST("/inventory/purchases/cancel", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), pageview.HTMLFragment, purchases.PurchaseCancel)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/cancel", "/api/inventory/purchase/update", http.MethodPost)

	pages.POST("/inventory/purchases/place", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), pageview.HTMLFragment, purchases.PurchasePlace)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/place", "/api/inventory/purchase/update", http.MethodPost)
	pages.POST("/inventory/purchases/exception", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), pageview.HTMLFragment, purchases.PurchaseException)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/exception", "/api/inventory/purchase/update", http.MethodPost)
	pages.POST("/inventory/purchases/resume", builtin.CasbinMiddlewareForPath("/api/inventory/purchase/update"), pageview.HTMLFragment, purchases.PurchaseResume)
	mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/inventory/purchases/resume", "/api/inventory/purchase/update", http.MethodPost)
}
