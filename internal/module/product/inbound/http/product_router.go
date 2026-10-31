package producthttp

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	inventorycontract "go_wp/internal/module/inventory/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	inventoryoutbound "go_wp/internal/module/product/outbound/inventory"
	publishrecordoutbound "go_wp/internal/module/product/outbound/publishrecord"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
)

const (
	ProductListPageTemplate    = "admin/product/products.html"
	ProductNewPageTemplate     = "admin/product/products_new.html"
	ProductEditPageTemplate    = "admin/product/product_edit.html"
	ProductDetailPageTemplate  = "admin/product/product_detail.html"
	ProductPricingPageTemplate = "admin/product/product_pricing.html"

	ProductPublishRecordPageTemplate  = "admin/product/product_publish_records.html"
	ProductDetailTemplatePageTemplate = "admin/product/product_detail_template.html"
	ProductAttributesPageTemplate     = "admin/product/product_attributes.html"
	ProductCategoriesPageTemplate     = "admin/product/product_categories.html"
	ProductBrandsPageTemplate         = "admin/product/product_brands.html"
	ProductTagsPageTemplate           = "admin/product/product_tags.html"
	ProductTranslationsPageTemplate   = "admin/product/product_translations.html"
)

func SetupProductRoutes(rg *permission.RouteGroup, db *gorm.DB, project projectcontract.ProjectService) (productcontract.ProductService, productcontract.ProductPageReader, *ProductHandle) {
	svc := productservice.NewService(productmodel.NewModel(db), project)
	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewProductHandle(svc)

	g := rg.Group("/product")

	registerProductCoreAPI(g, handle)
	registerProductDetailTemplateAPI(g, handle)
	registerProductAttributeAPI(g, handle)
	registerProductTaxonomyAPI(g, handle)
	registerProductTagAPI(g, handle)
	registerProductPricingAPI(g, handle)
	registerProductPublishHistoryAPI(g, NewPublishAttemptHandle(svc))
	return svc, svc, handle
}

func SetupProductPublicRoutes(router *gin.Engine, svc productcontract.PublicVariantReader) {
	permission.NewRouteGroup(router.Group("")).GET("/api/site/product/variants", permission.Exempt, builtin.RequestRateLimitMiddleware(90, time.Minute), NewProductPublicHandle(svc).Variants)
}

func SetupProductCatalogRoutes(router *gin.Engine, svc productcontract.PublicCatalogReader) {
	permission.NewRouteGroup(router.Group("")).GET("/api/site/product/catalog", permission.Exempt, builtin.RequestRateLimitMiddleware(90, time.Minute), NewProductCatalogHandle(svc).List)
}

func registerProductPublishHistoryAPI(g *permission.RouteGroup, handle *PublishAttemptHandle) {
	g.GET("/publish-attempts", permission.ProductPublishAttempts, handle.ProductPublishAttempts)
}

func registerProductCoreAPI(g *permission.RouteGroup, handle *ProductHandle) {

	g.GET("/list", permission.ProductList, handle.List)
	g.GET("/get", permission.ProductGet, handle.Get)
	g.POST("/create", permission.ProductCreate, handle.Create)
	g.POST("/update", permission.ProductUpdate, handle.Update)
	g.POST("/delete", permission.ProductDelete, handle.Delete)

	g.POST("/bulk-delete", permission.ProductBulkDelete, handle.BulkDelete)
	g.POST("/variant/create", permission.ProductVariantCreate, handle.CreateVariant)

	g.POST("/variant/generate", permission.ProductVariantGenerate, handle.GenerateVariants)
	g.POST("/variant/update", permission.ProductVariantUpdate, handle.UpdateVariant)
	g.POST("/variant/delete", permission.ProductVariantDelete, handle.DeleteVariant)

	g.POST("/variant/save", permission.ProductVariantSave, handle.SaveVariantList)

	g.POST("/rating/add", permission.ProductRatingAdd, handle.AddRating)
	g.POST("/rating/delete", permission.ProductRatingDelete, handle.DeleteRating)
}

func registerProductDetailTemplateAPI(g *permission.RouteGroup, handle *ProductHandle) {

	g.POST("/detail-template/reapply-preset", permission.ProductDetailTemplateReapply, handle.ReapplyDetailTemplatePreset)
	g.POST("/detail-template/rollback-document", permission.ProductDetailTemplateRollback, handle.RollbackDetailTemplateDocument)
	g.POST("/detail-template/create", permission.ProductDetailTemplateCreate, handle.CreateDetailTemplate)
	g.POST("/detail-template/publish", permission.ProductDetailTemplatePublish, handle.PublishDetailTemplate)
	g.POST("/detail-template/apply", permission.ProductDetailTemplateApply, handle.ApplyDetailTemplate)
	g.POST("/detail-template/update-url", permission.ProductDetailTemplateUpdateURL, handle.UpdateDetailTemplateURL)
}

func registerProductAttributeAPI(g *permission.RouteGroup, handle *ProductHandle) {

	g.GET("/attribute/list", permission.ProductAttributeList, handle.ListAttributes)
	g.GET("/attribute/get", permission.ProductAttributeGet, handle.GetAttribute)
	g.POST("/attribute/create", permission.ProductAttributeCreate, handle.CreateAttribute)
	g.POST("/attribute/update", permission.ProductAttributeUpdate, handle.UpdateAttribute)
	g.POST("/attribute/set-values", permission.ProductAttributeSetValues, handle.SetAttributeValues)
	g.POST("/attribute/delete", permission.ProductAttributeDelete, handle.DeleteAttribute)

	g.POST("/attribute/bulk-delete", permission.ProductAttributeBulkDelete, handle.BulkDeleteAttributes)
}

func registerProductTaxonomyAPI(g *permission.RouteGroup, handle *ProductHandle) {

	g.GET("/category/list", permission.ProductCategoryList, handle.ListCategories)
	g.GET("/category/parents", permission.ProductCategoryParents, handle.ProductCategoryParents)
	g.GET("/category/get", permission.ProductCategoryGet, handle.GetCategory)
	g.POST("/category/create", permission.ProductCategoryCreate, handle.CreateCategory)
	g.POST("/category/update", permission.ProductCategoryUpdate, handle.UpdateCategory)
	g.POST("/category/delete", permission.ProductCategoryDelete, handle.DeleteCategory)
	g.POST("/category/bulk-delete", permission.ProductCategoryBulkDelete, handle.BulkDeleteCategories)
	g.GET("/brand/list", permission.ProductBrandList, handle.ListBrands)
	g.GET("/brand/get", permission.ProductBrandGet, handle.GetBrand)
	g.POST("/brand/create", permission.ProductBrandCreate, handle.CreateBrand)
	g.POST("/brand/update", permission.ProductBrandUpdate, handle.UpdateBrand)
	g.POST("/brand/delete", permission.ProductBrandDelete, handle.DeleteBrand)
	g.POST("/brand/bulk-delete", permission.ProductBrandBulkDelete, handle.BulkDeleteBrands)
}

func registerProductTagAPI(g *permission.RouteGroup, handle *ProductHandle) {

	g.GET("/tag/list", permission.ProductTagList, handle.ListTags)
	g.GET("/tag/get", permission.ProductTagGet, handle.GetTag)
	g.GET("/tag/products", permission.ProductTagProducts, handle.ListTagProducts)
	g.GET("/tag/rule-types", permission.ProductTagRuleTypes, handle.ListTagRuleTypes)
	g.POST("/tag/create", permission.ProductTagCreate, handle.CreateTag)
	g.POST("/tag/update", permission.ProductTagUpdate, handle.UpdateTag)
	g.POST("/tag/delete", permission.ProductTagDelete, handle.DeleteTag)
	g.POST("/tag/bulk-delete", permission.ProductTagBulkDelete, handle.BulkDeleteTags)
	g.POST("/tag/recalc", permission.ProductTagRecalc, handle.RecalcTags)
}

func registerProductPricingAPI(g *permission.RouteGroup, handle *ProductHandle) {

	g.GET("/pricing/rules", permission.ProductPricingRules, handle.ListPricingRuleTypes)
	g.GET("/pricing/roundings", permission.ProductPricingRoundings, handle.ListPricingRoundingOptions)
	g.POST("/pricing/preview", permission.ProductPricingPreview, handle.PreviewPricing)
	g.POST("/pricing/apply", permission.ProductPricingApply, handle.ApplyPricing)

	g.POST("/bulk-pricing", permission.ProductBulkPricing, handle.BulkPricing)
	g.GET("/pricing/history", permission.ProductPricingHistory, handle.ListPriceAdjustments)
	g.GET("/pricing/adjustment", permission.ProductPricingAdjustment, handle.GetPriceAdjustment)
}

func SetupProductPages(pages *gin.RouterGroup, api *permission.RouteGroup, writeHandle *ProductHandle,
	products productcontract.ProductService,
	reader productcontract.ProductPageReader,
	projects projectcontract.ProjectService,
	contentTemplates contenttemplatecontract.ContentTemplateService,
	presentations presentationcontract.PresentationService,
	inventories inventorycontract.InventoryService,
	pageSvc pagecontract.PageService,
	contentSvc contentcontract.ContentService) *ProductPageHandle {
	if pages == nil {
		return nil
	}

	productPages := NewProductPageHandle(products, reader)

	productPages.SetDetailTemplateDeps(contentTemplates, presentations)

	productPages.SetInventoryDeps(inventories)

	productPages.SetDetailTemplateModePort(presentations)

	productPages.SetPublishRecordDeps(presentations, presentations)

	writeHandle.SetDetailTemplateWriteDeps(productservice.DetailTemplateDeps{
		Templates: contentTemplates, Instances: presentations, Products: products, Projects: projects,
	})

	productPages.SetSeoTitleSources(pageSvc, contentSvc)

	registerProductReadAPI(api.Group("/product"), productPages)
	registerProductListPages(pages, productPages)
	registerProductTemplatePages(pages, productPages)
	registerProductAttributePages(pages, productPages)
	registerProductTaxonomyPages(pages, productPages)
	registerProductTagPages(pages, productPages)
	registerProductPricingPages(pages, productPages)
	return productPages
}

func registerProductReadAPI(g *permission.RouteGroup, handle *ProductPageHandle) {
	g.POST("/variant/preview", permission.ProductVariantPreview, handle.PreviewVariant)
	g.POST("/seo-score", permission.ProductSEOScore, handle.SeoScore)
	g.POST("/category/seo-score", permission.ProductCategorySEOScore, handle.CategorySeoScore)
	g.POST("/brand/seo-score", permission.ProductBrandSEOScore, handle.BrandSeoScore)
}

func registerProductListPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/products", adminui.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductsPage)

	pages.GET("/products/new", adminui.PageAuthzAs("/api/product/create", http.MethodPost), pageview.UseRenderer(adminui.RenderPage), productPages.ProductNewPage)

	pages.GET("/products/detail", adminui.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductDetailPage)

	pages.GET("/products/edit", adminui.PageAuthzAs("/api/product/update", http.MethodPost), pageview.UseRenderer(adminui.RenderPage), productPages.ProductEditPage)

	pages.GET("/products/publish-records", adminui.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductPublishRecordPage)

	pages.GET("/products/preview-frame", adminui.PageAuthzAs("/api/presentation/preview", http.MethodPost), productPages.ProductPreviewFrame)
}

func registerProductTemplatePages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/products/template", adminui.PageAuthz("/api/product/get"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductDetailTemplatePage)
	pages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), productPages.ProductDetailTemplatePreview)
}

func registerProductAttributePages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-attributes", adminui.PageAuthz("/api/product/attribute/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductAttributesPage)
}

func registerProductTaxonomyPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-categories", adminui.PageAuthz("/api/product/category/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductCategoriesPage)

	pages.GET("/product-brands", adminui.PageAuthz("/api/product/brand/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductBrandsPage)

}

func registerProductTagPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-tags", adminui.PageAuthz("/api/product/tag/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductTagsPage)

}

func registerProductPricingPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-pricing", adminui.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductPricingPage)

}

func SetupProductTranslationRoutes(apiRoot *permission.RouteGroup, adminPages *gin.RouterGroup,
	products productcontract.ProductService, pages pagecontract.PageService,
	instances ProductTranslationInstancePort) *ProductTranslationHandle {
	handle := NewProductTranslationHandle(products, pages, instances)
	adminPages.GET("/products/translations", adminui.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), handle.ProductTranslations)

	apiRoot.Group("/product").POST("/translations/save", permission.ProductTranslationsSave, handle.SaveProductTranslations)
	return handle
}

const variantSelectionPrefix = "attr:"

type ProductPageHandle struct {
	products productcontract.ProductService

	reader productcontract.ProductPageReader

	templates contenttemplatecontract.ContentTemplateService
	instances ProductDetailTemplatePort

	modePort ProductDetailTemplateModePort

	warehouses      productcontract.WarehouseOptionsPort
	entityTemplates productcontract.EntityTemplatePort

	seoPages    pagecontract.PageService
	seoContents contentcontract.ContentService
}

func (h *ProductPageHandle) SetPublishRecordDeps(instances presentationcontract.PublishRecordPort,
	attempts presentationcontract.PublishAttemptPort) {
	port := publishrecordoutbound.NewWithAttempts(instances, attempts)
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetPublishRecordPort(port)
		wiring.SetPublishAttemptPort(port)
	}
}

type productPagePortWiring interface {
	SetWarehouseOptionsPort(productcontract.WarehouseOptionsPort)
	SetEntityTemplatePort(productcontract.EntityTemplatePort)
	SetDetailTemplatePort(productcontract.DetailTemplatePort)
	SetPublishRecordPort(productcontract.PublishRecordReader)

	SetPublishAttemptPort(productcontract.PublishAttemptReader)
}

func NewProductPageHandle(products productcontract.ProductService, reader productcontract.ProductPageReader) *ProductPageHandle {
	return &ProductPageHandle{products: products, reader: reader}
}

func (h *ProductPageHandle) SetInventoryDeps(inventory inventorycontract.InventoryService) {
	port := inventoryoutbound.New(inventory)
	h.warehouses = port
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetWarehouseOptionsPort(port)
	}
}

type ProductHandle struct {
	svc          productcontract.ProductService
	templateDeps productservice.DetailTemplateDeps
	modePort     presentationcontract.DetailTemplateModePort
}

func NewProductHandle(svc productcontract.ProductService) *ProductHandle {
	return &ProductHandle{svc: svc}
}

func SetupProductOptionsRoutes(router *gin.Engine, svc productcontract.PublicProductOptionsReader) {
	permission.NewRouteGroup(router.Group("")).GET("/api/site/product/options", permission.Exempt, builtin.RequestRateLimitMiddleware(90, time.Minute), (&ProductOptionsHandle{svc: svc}).Get)
}
