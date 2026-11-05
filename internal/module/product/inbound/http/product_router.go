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
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	inventoryoutbound "go_wp/internal/module/product/outbound/inventory"
	publishrecordoutbound "go_wp/internal/module/product/outbound/publishrecord"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
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

// SetupProductRoutes 装配商品模块：审计变更属于必需依赖，随构造一起给齐；
// 发布相关的 setter 仍是装配期的接线点，由 routers 侧按模块就绪顺序接。
func SetupProductRoutes(rg *mw.RouteGroup, db *gorm.DB, project projectcontract.ProjectService,
	changes masterdatacontract.MasterDataService) (productcontract.ProductService, productcontract.ProductPageReader, *ProductHandle) {
	svc := productservice.NewService(productmodel.NewModel(db), project, changes)
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
	mw.NewRouteGroup(router.Group(""), mw.RouteExempt).GET("/api/site/product/variants", "", builtin.RequestRateLimitMiddleware(90, time.Minute), NewProductPublicHandle(svc).Variants)
}

func SetupProductCatalogRoutes(router *gin.Engine, svc productcontract.PublicCatalogReader) {
	mw.NewRouteGroup(router.Group(""), mw.RouteExempt).GET("/api/site/product/catalog", "", builtin.RequestRateLimitMiddleware(90, time.Minute), NewProductCatalogHandle(svc).List)
}

func registerProductPublishHistoryAPI(g *mw.RouteGroup, handle *PublishAttemptHandle) {
	g.GET("/publish-attempts", productenums.ProductPublishAttempts, pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.ProductPublishAttempts)
}

func registerProductCoreAPI(g *mw.RouteGroup, handle *ProductHandle) {

	g.GET("/list", productenums.ProductList, handle.List)
	g.GET("/get", productenums.ProductGet, handle.Get)
	g.POST("/create", productenums.ProductCreate, pageview.HTMLFragment, handle.Create)
	g.POST("/update", productenums.ProductUpdate, pageview.HTMLFragment, handle.Update)
	g.POST("/delete", productenums.ProductDelete, pageview.HTMLFragment, handle.Delete)

	g.POST("/bulk-delete", productenums.ProductBulkDelete, pageview.HTMLFragment, handle.BulkDelete)
	g.POST("/variant/create", productenums.ProductVariantCreate, pageview.HTMLFragment, handle.CreateVariant)

	g.POST("/variant/generate", productenums.ProductVariantGenerate, handle.GenerateVariants)
	g.POST("/variant/update", productenums.ProductVariantUpdate, handle.UpdateVariant)
	g.POST("/variant/delete", productenums.ProductVariantDelete, pageview.HTMLFragment, handle.DeleteVariant)

	g.POST("/variant/save", productenums.ProductVariantSave, handle.SaveVariantList)

	g.POST("/rating/add", productenums.ProductRatingAdd, pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.AddRating)
	g.POST("/rating/delete", productenums.ProductRatingDelete, pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.DeleteRating)
}

func registerProductDetailTemplateAPI(g *mw.RouteGroup, handle *ProductHandle) {

	g.POST("/detail-template/reapply-preset", productenums.ProductDetailTemplateReapply, pageview.HTMLFragment, handle.ReapplyDetailTemplatePreset)
	g.POST("/detail-template/rollback-document", productenums.ProductDetailTemplateRollback, pageview.HTMLFragment, handle.RollbackDetailTemplateDocument)
	g.POST("/detail-template/create", productenums.ProductDetailTemplateCreate, pageview.HTMLFragment, handle.CreateDetailTemplate)
	g.POST("/detail-template/publish", productenums.ProductDetailTemplatePublish, pageview.HTMLFragment, handle.PublishDetailTemplate)
	g.POST("/detail-template/apply", productenums.ProductDetailTemplateApply, pageview.HTMLFragment, handle.ApplyDetailTemplate)
	g.POST("/detail-template/update-url", productenums.ProductDetailTemplateUpdateURL, pageview.HTMLFragment, handle.UpdateDetailTemplateURL)
}

func registerProductAttributeAPI(g *mw.RouteGroup, handle *ProductHandle) {

	g.GET("/attribute/list", productenums.ProductAttributeList, handle.ListAttributes)
	g.GET("/attribute/get", productenums.ProductAttributeGet, handle.GetAttribute)
	g.POST("/attribute/create", productenums.ProductAttributeCreate, pageview.HTMLFragment, handle.CreateAttribute)
	g.POST("/attribute/update", productenums.ProductAttributeUpdate, pageview.HTMLFragment, handle.UpdateAttribute)
	g.POST("/attribute/set-values", productenums.ProductAttributeSetValues, pageview.HTMLFragment, handle.SetAttributeValues)
	g.POST("/attribute/delete", productenums.ProductAttributeDelete, pageview.HTMLFragment, handle.DeleteAttribute)

	g.POST("/attribute/bulk-delete", productenums.ProductAttributeBulkDelete, pageview.HTMLFragment, handle.BulkDeleteAttributes)
}

func registerProductTaxonomyAPI(g *mw.RouteGroup, handle *ProductHandle) {

	g.GET("/category/list", productenums.ProductCategoryList, handle.ListCategories)
	g.GET("/category/parents", productenums.ProductCategoryParents, handle.ProductCategoryParents)
	g.GET("/category/get", productenums.ProductCategoryGet, handle.GetCategory)
	g.POST("/category/create", productenums.ProductCategoryCreate, pageview.HTMLFragment, handle.CreateCategory)
	g.POST("/category/update", productenums.ProductCategoryUpdate, pageview.HTMLFragment, handle.UpdateCategory)
	g.POST("/category/delete", productenums.ProductCategoryDelete, pageview.HTMLFragment, handle.DeleteCategory)
	g.POST("/category/bulk-delete", productenums.ProductCategoryBulkDelete, pageview.HTMLFragment, handle.BulkDeleteCategories)
	g.GET("/brand/list", productenums.ProductBrandList, handle.ListBrands)
	g.GET("/brand/get", productenums.ProductBrandGet, handle.GetBrand)
	g.POST("/brand/create", productenums.ProductBrandCreate, pageview.HTMLFragment, handle.CreateBrand)
	g.POST("/brand/update", productenums.ProductBrandUpdate, pageview.HTMLFragment, handle.UpdateBrand)
	g.POST("/brand/delete", productenums.ProductBrandDelete, pageview.HTMLFragment, handle.DeleteBrand)
	g.POST("/brand/bulk-delete", productenums.ProductBrandBulkDelete, pageview.HTMLFragment, handle.BulkDeleteBrands)
}

func registerProductTagAPI(g *mw.RouteGroup, handle *ProductHandle) {

	g.GET("/tag/list", productenums.ProductTagList, handle.ListTags)
	g.GET("/tag/get", productenums.ProductTagGet, handle.GetTag)
	g.GET("/tag/products", productenums.ProductTagProducts, handle.ListTagProducts)
	g.GET("/tag/rule-types", productenums.ProductTagRuleTypes, handle.ListTagRuleTypes)
	g.POST("/tag/create", productenums.ProductTagCreate, pageview.HTMLFragment, handle.CreateTag)
	g.POST("/tag/update", productenums.ProductTagUpdate, pageview.HTMLFragment, handle.UpdateTag)
	g.POST("/tag/delete", productenums.ProductTagDelete, pageview.HTMLFragment, handle.DeleteTag)
	g.POST("/tag/bulk-delete", productenums.ProductTagBulkDelete, pageview.HTMLFragment, handle.BulkDeleteTags)
	g.POST("/tag/recalc", productenums.ProductTagRecalc, pageview.HTMLFragment, handle.RecalcTags)
}

func registerProductPricingAPI(g *mw.RouteGroup, handle *ProductHandle) {

	g.GET("/pricing/rules", productenums.ProductPricingRules, handle.ListPricingRuleTypes)
	g.GET("/pricing/roundings", productenums.ProductPricingRoundings, handle.ListPricingRoundingOptions)
	g.POST("/pricing/preview", productenums.ProductPricingPreview, pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.PreviewPricing)
	g.POST("/pricing/apply", productenums.ProductPricingApply, pageview.HTMLFragment, handle.ApplyPricing)

	g.POST("/bulk-pricing", productenums.ProductBulkPricing, pageview.HTMLFragment, handle.BulkPricing)
	g.GET("/pricing/history", productenums.ProductPricingHistory, handle.ListPriceAdjustments)
	g.GET("/pricing/adjustment", productenums.ProductPricingAdjustment, handle.GetPriceAdjustment)
}

func SetupProductPages(pages *gin.RouterGroup, api *mw.RouteGroup, writeHandle *ProductHandle,
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

	productPages.SetPublishRecordDeps(presentations)

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

func registerProductReadAPI(g *mw.RouteGroup, handle *ProductPageHandle) {
	g.POST("/variant/preview", productenums.ProductVariantPreview, handle.PreviewVariant)
	g.POST("/seo-score", productenums.ProductSEOScore, handle.SeoScore)
	g.POST("/category/seo-score", productenums.ProductCategorySEOScore, handle.CategorySeoScore)
	g.POST("/brand/seo-score", productenums.ProductBrandSEOScore, handle.BrandSeoScore)
}

func registerProductListPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/products", mw.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductsPage)

	pages.GET("/products/new", mw.PageAuthzAs("/api/product/create", http.MethodPost), pageview.UseRenderer(adminui.RenderPage), productPages.ProductNewPage)

	pages.GET("/products/detail", mw.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductDetailPage)

	pages.GET("/products/edit", mw.PageAuthzAs("/api/product/update", http.MethodPost), pageview.UseRenderer(adminui.RenderPage), productPages.ProductEditPage)

	pages.GET("/products/publish-records", mw.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductPublishRecordPage)

	pages.GET("/products/preview-frame", mw.PageAuthzAs("/api/presentation/preview", http.MethodPost), productPages.ProductPreviewFrame)
}

func registerProductTemplatePages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/products/template", mw.PageAuthz("/api/product/get"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductDetailTemplatePage)
	pages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), productPages.ProductDetailTemplatePreview)
}

func registerProductAttributePages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-attributes", mw.PageAuthz("/api/product/attribute/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductAttributesPage)
}

func registerProductTaxonomyPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-categories", mw.PageAuthz("/api/product/category/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductCategoriesPage)

	pages.GET("/product-brands", mw.PageAuthz("/api/product/brand/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductBrandsPage)

}

func registerProductTagPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-tags", mw.PageAuthz("/api/product/tag/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductTagsPage)

}

func registerProductPricingPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {

	pages.GET("/product-pricing", mw.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), productPages.ProductPricingPage)

}

func SetupProductTranslationRoutes(apiRoot *mw.RouteGroup, adminPages *gin.RouterGroup,
	products productcontract.ProductService, pages pagecontract.PageService,
	instances ProductTranslationInstancePort) *ProductTranslationHandle {
	handle := NewProductTranslationHandle(products, pages, instances)
	adminPages.GET("/products/translations", mw.PageAuthz("/api/product/list"), pageview.UseRenderer(adminui.RenderPage), handle.ProductTranslations)

	apiRoot.Group("/product").POST("/translations/save", productenums.ProductTranslationsSave, pageview.HTMLFragment, pageview.UseRenderer(adminui.RenderPage), handle.SaveProductTranslations)
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

func (h *ProductPageHandle) SetPublishRecordDeps(attempts presentationcontract.PublishAttemptPort) {
	port := publishrecordoutbound.NewWithAttempts(attempts)
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetPublishAttemptPort(port)
	}
}

type productPagePortWiring interface {
	SetWarehouseOptionsPort(productcontract.WarehouseOptionsPort)
	SetEntityTemplatePort(productcontract.EntityTemplatePort)
	SetDetailTemplatePort(productcontract.DetailTemplatePort)

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
	mw.NewRouteGroup(router.Group(""), mw.RouteExempt).GET("/api/site/product/options", "", builtin.RequestRateLimitMiddleware(90, time.Minute), (&ProductOptionsHandle{svc: svc}).Get)
}
