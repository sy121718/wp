package producthttp

// product_router.go — product 模块**唯一的**路由声明点，同时也是模块装配入口。
//
// 两条硬规则（与 page 模块同一套形状）：
//
//  1. **路由只有一份**。本模块全部入口 —— `/api/product/*` 的接口面与 `/admin/*` 的页面面
//     —— 都只在这个文件里声明。以前拆成 product_router.go（API）+ product_page_router.go
//     （页面）两份，于是回答「这个模块一共暴露了哪些入口」要跨两个文件拼，而路由正是权限点 /
//     CSRF / Casbin 的对账面：漏看一条的表现不是编译错误，而是「接口能删、页面删不掉」这类
//     两边都不报错、测试也不红的分叉。**按业务域拆的是控制器**（product_subject_handle.go 商品主体：
//     CRUD / 变体 / 列表与编辑页 / 捆绑配置 / 定价；product_taxonomy_handle.go 分类与品牌；
//     product_tag_handle.go 标签；product_attributes_handle.go 属性；product_detail_template_handle.go 详情页模板；
//     product_score_handle.go SEO 评分；product_translations_handle.go 译文；
//     product_bulk_handle.go 批量回执文案与 id 归一）。同一域里接口面在前、页面面在后，
//     所以找一个动作只需认一个文件。
//
//  2. **同一功能的页面与接口挨着写**。一个业务动作有两条通道：JSON 接口（工作台 / 脚本 /
//     批量 / 构建期）与后台页面（运营）。它们在权限点路径、拒绝规则上必须同源，所以下面按
//     业务域分节，每节接口面在前、页面面在后。
//
// 中间件链：`/api/product` 组走 authorizedAPI 的三层链（SessionAuth + CSRF + Casbin，
// 权限点作为第二实参声明，供 check-permission-gaps.sh 对账）；`/admin` 页面组的
// Session + CSRF + 权限上下文由装配层统一挂好，**页面写动作一律按「对应接口的路径」显式挂
// Casbin**（页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表里没有那条路径
// 而拒绝所有用户，含超管）。
//
// 只读片段（变体预览、SEO 评分、属性值行、成员解析、标签命中）**不挂 Casbin**：它们
// 不落库、不写产物，页面组已有 Session + CSRF；给只读片段单挂权限点反而会造出
// 「能看编辑页却打不开评分」的状态。这一条在下面的注释里逐处写明。

import (
	"net/http"

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
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
)

// ── 页面模板清单（UI 线的装配参数）──
//
// 模板名归**路由**而不是控制器：控制器只交出视图（any），渲染与模板选择由 adminui.Page /
// adminui.Bare 这一侧决定。于是「这个模块有哪些页面」在本文件里能一次读全，
// 而 handle 文件里不再出现模板字符串（门禁 TestControllerShape 的 exit 项）。
//
// 导出是给测试用的：feature 测试自己起 gin 引擎注册路由，包一层 adminui.Page 时
// 需要同一份模板名 —— 让测试抄一遍字符串就会与生产漂移。
const (
	ProductListPageTemplate    = "admin/product/products.html"
	ProductNewPageTemplate     = "admin/product/products_new.html"
	ProductEditPageTemplate    = "admin/product/product_edit.html"
	ProductDetailPageTemplate  = "admin/product/product_detail.html"
	ProductPricingPageTemplate = "admin/product/product_pricing.html"
	// ProductPublishRecordPageTemplate 商品发布记录页（B9）：当前发布状态一览，只读。
	ProductPublishRecordPageTemplate  = "admin/product/product_publish_records.html"
	ProductDetailTemplatePageTemplate = "admin/product/product_detail_template.html"
	ProductAttributesPageTemplate     = "admin/product/product_attributes.html"
	ProductAttributeRowsTemplate      = "admin/product/product_attribute_rows.html"
	ProductCategoriesPageTemplate     = "admin/product/product_categories.html"
	ProductCategoryParentsTemplate    = "admin/product/product_category_parent_options.html"
	ProductBrandsPageTemplate         = "admin/product/product_brands.html"
	ProductTagsPageTemplate           = "admin/product/product_tags.html"
	ProductTagHitsTemplate            = "admin/product/product_tag_hits.html"
	ProductTranslationsPageTemplate   = "admin/product/product_translations.html"
	EntitySeoDrawerTemplate           = "admin/product/entity_seo_drawer.html"
	SeoScoreFragmentTemplate          = "fragments/seo_score"
	// ProductPreviewNoticeTemplate 预览 iframe 的替代页（商品与文章共用这一份）。
	ProductPreviewNoticeTemplate = "admin/preview_notice.html"
)

// ══ 接口面装配（authorizedAPI 三层链）══

// SetupProductRoutes 装配 product 模块**接口面**（`/api/product/*`），返回模块契约。
// project 用于解析商品所属工程（products.project_id 为 NOT NULL 外键）。
//
// 装配同时注入内容译文读取端口（sys_translation）：构建期商品可翻译字段
// （name/subtitle/description）按构建语言取译文，语境 product.<字段名>。
// 端口在这里注入是因为本模块的 service 不持有 *gorm.DB（表隔离约定）。
//
// issue #32：归属仓解析与库存记录生成不再经跨模块端口 —— 商品与库存同属一个模块，
// 装配时把库存 service 直接交给商品用例（传 nil 表示不生成库存记录，纯商品单测路径）。
// 返回 (接口面契约, 页面读模型)。两个返回值都是**同一个 service**、两个静态类型：
// 读模型（contract.ProductPageReader）只有页面面用得到，而页面装配
// （assembly_publish.go 的 SetupProductPages）与接口面装配在不同方法里 —— 由装配层
// 把这两条一起往下传，页面处理器就不必在请求期断言，漏接变成编译错误。
func SetupProductRoutes(rg *permission.RouteGroup, db *gorm.DB, project projectcontract.ProjectService) (productcontract.ProductService, productcontract.ProductPageReader) {
	svc := productservice.NewService(productmodel.NewModel(db), project)
	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewProductHandle(svc)

	g := rg.Group("/product")
	// 取词注入：service 层的展示文案（内置规则名 / 描述等）按请求语言渲染。
	g.Use(productTranslateMiddleware())

	registerProductCoreAPI(g, handle)
	registerProductDetailTemplateAPI(g, handle)
	registerProductAttributeAPI(g, handle)
	registerProductTaxonomyAPI(g, handle)
	registerProductTagAPI(g, handle)
	registerProductPricingAPI(g, handle)
	registerProductPublishHistoryAPI(g, NewPublishAttemptHandle(svc))
	return svc, svc
}

// registerProductPublishHistoryAPI 发布历史（迁移 673）。
//
// 单独一条权限点而不是复用 product:get / product:list：这条交的是**谁在什么时候点了发布、
// 失败原因原文**（运维向审计信息）。混进「能看商品」的权限里，只该看商品的人会顺手
// 看到全站发布失败堆栈。
//
// 出口是 JSON 而不是 HTML 片段：DOM 由发布记录页里已渲染的 Jet <template> 填充
//（ui/json_swap.js 的同源机制，见 internal/templates/CLAUDE.md）。
func registerProductPublishHistoryAPI(g *permission.RouteGroup, handle *PublishAttemptHandle) {
	g.GET("/publish-attempts", permission.ProductPublishAttempts, handle.ProductPublishAttempts)
}

// registerProductCoreAPI 注册 商品本体（含变体） 的接口路由。
//
// 权限点逐条独立：API 组按实际请求路径 enforce，而 sys_permission 上 permission_code
// 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
func registerProductCoreAPI(g *permission.RouteGroup, handle *ProductHandle) {
	// —— 商品本体（含变体）——
	g.GET("/list", permission.ProductList, handle.List)
	g.GET("/get", permission.ProductGet, handle.Get)
	g.POST("/create", permission.ProductCreate, handle.Create)
	g.POST("/update", permission.ProductUpdate, handle.Update)
	g.POST("/delete", permission.ProductDelete, handle.Delete)
	// 批量删除：与单条同一个权限点、同一个 service 方法，回执从「整页提示」换成
	// JSON（ui/write-base.js 基座消费 data.deleted / data.skipped / data.items[].message）。
	g.POST("/bulk-delete", permission.ProductBulkDelete, handle.BulkDelete)
	g.POST("/variant/create", permission.ProductVariantCreate, handle.CreateVariant)
	// 组合生成（issue #8）：勾选属性值 → 笛卡尔积写变体（幂等，超上限整体拒绝）。
	g.POST("/variant/generate", permission.ProductVariantGenerate, handle.GenerateVariants)
	g.POST("/variant/update", permission.ProductVariantUpdate, handle.UpdateVariant)
	g.POST("/variant/delete", permission.ProductVariantDelete, handle.DeleteVariant)
	// 清单保存（整张清单一次提交，前端从清单 DOM 组装 rows）：权限点与组合生成分开 ——
	// API 组按实际请求路径 enforce，sys_permission 上 permission_code 唯一，复用会让其中
	// 一条路径拿不到策略。
	g.POST("/variant/save", permission.ProductVariantSave, handle.SaveVariantList)
	// 评分是商品的子资源（issue #33）：补录 / 删除各一条记录，不碰商品自己的列。
	// 两个动作各自的权限点（理由同上：一个 code 只能绑一条 API 路由）。
	g.POST("/rating/add", permission.ProductRatingAdd, handle.AddRating)
	g.POST("/rating/delete", permission.ProductRatingDelete, handle.DeleteRating)
}

// registerProductDetailTemplateAPI 注册 详情页模板的写动作（页面 /admin/products/template） 的接口路由。
//
// 权限点逐条独立：API 组按实际请求路径 enforce，而 sys_permission 上 permission_code
// 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
func registerProductDetailTemplateAPI(g *permission.RouteGroup, handle *ProductHandle) {
	// —— 详情页模板的写动作（页面 /admin/products/template）——
	// 双轨（迁移 282）：重新套用预设（放弃独立文档）与文档级回滚（取历史快照重发）；
	// 外加页面上的新建命名模板 / 首次发布 / 切换模板 / 改 URL。
	// 每个动作一条独立权限点：API 组按实际请求路径 enforce，而 sys_permission 上
	// permission_code 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
	// 能力由已注入的 deps 提供，装配点见文件末尾的 Bind*。
	g.POST("/detail-template/reapply-preset", permission.ProductDetailTemplateReapply, handle.ReapplyDetailTemplatePreset)
	g.POST("/detail-template/rollback-document", permission.ProductDetailTemplateRollback, handle.RollbackDetailTemplateDocument)
	g.POST("/detail-template/create", permission.ProductDetailTemplateCreate, handle.CreateDetailTemplate)
	g.POST("/detail-template/publish", permission.ProductDetailTemplatePublish, handle.PublishDetailTemplate)
	g.POST("/detail-template/apply", permission.ProductDetailTemplateApply, handle.ApplyDetailTemplate)
	g.POST("/detail-template/update-url", permission.ProductDetailTemplateUpdateURL, handle.UpdateDetailTemplateURL)
}

// registerProductAttributeAPI 注册 属性组与属性值（issue #7） 的接口路由。
//
// 权限点逐条独立：API 组按实际请求路径 enforce，而 sys_permission 上 permission_code
// 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
func registerProductAttributeAPI(g *permission.RouteGroup, handle *ProductHandle) {
	// —— 属性组与属性值（issue #7）——
	// 属性组可跨商品复用，故独立于商品资源。
	// 属性值用 set-values 整体保存（全量替换），比逐行接口少一半请求、也避免半截状态。
	g.GET("/attribute/list", permission.ProductAttributeList, handle.ListAttributes)
	g.GET("/attribute/get", permission.ProductAttributeGet, handle.GetAttribute)
	g.POST("/attribute/create", permission.ProductAttributeCreate, handle.CreateAttribute)
	g.POST("/attribute/update", permission.ProductAttributeUpdate, handle.UpdateAttribute)
	g.POST("/attribute/set-values", permission.ProductAttributeSetValues, handle.SetAttributeValues)
	g.POST("/attribute/delete", permission.ProductAttributeDelete, handle.DeleteAttribute)
	// 批量删除：与单条同一个权限点（删的都是属性组，只是形状从 {id} 变成 {ids}），
	// 回执形状与商品批量删除逐字一致（write-base.js 按 outcome=delete 读 data.deleted）。
	g.POST("/attribute/bulk-delete", permission.ProductAttributeBulkDelete, handle.BulkDeleteAttributes)
}

// registerProductTaxonomyAPI 注册 分类与品牌（issue #10） 的接口路由。
//
// 权限点逐条独立：API 组按实际请求路径 enforce，而 sys_permission 上 permission_code
// 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
func registerProductTaxonomyAPI(g *permission.RouteGroup, handle *ProductHandle) {
	// —— 分类与品牌（issue #10）——
	// 分类是树（list 返回树、get 取单节点），品牌是平铺列表。
	// 分类与品牌都可跨商品复用，故与商品资源并列而不是嵌在商品路径下。
	g.GET("/category/list", permission.ProductCategoryList, handle.ListCategories)
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

// registerProductTagAPI 注册 标签与自动规则（issue #11） 的接口路由。
//
// 权限点逐条独立：API 组按实际请求路径 enforce，而 sys_permission 上 permission_code
// 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
func registerProductTagAPI(g *permission.RouteGroup, handle *ProductHandle) {
	// —— 标签与自动规则（issue #11）——
	// 手工标签手工挂载，自动标签按内置规则类型 + 参数维护，归属由明确的重算时机更新；
	// products 是「某标签命中哪些商品」（后台核对）。标签可跨商品复用。
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

// registerProductPricingAPI 注册 定价工具（issue #13） 的接口路由。
//
// 权限点逐条独立：API 组按实际请求路径 enforce，而 sys_permission 上 permission_code
// 唯一 —— 一个 code 挂两条路径会把其中一条挤出策略（含超管全员 403）。
func registerProductPricingAPI(g *permission.RouteGroup, handle *ProductHandle) {
	// —— 定价工具（issue #13）——
	// 四种内置规则 + 尾数处理，可对单个 SKU / 单商品全部变体 / 筛选集批量应用。
	// preview 与 apply 共用同一份规则入参（预览不落库，应用落库 + 留痕）。
	// 结果写回 product_variants.price，不参与构建期计算。
	g.GET("/pricing/rules", permission.ProductPricingRules, handle.ListPricingRuleTypes)
	g.GET("/pricing/roundings", permission.ProductPricingRoundings, handle.ListPricingRoundingOptions)
	g.POST("/pricing/preview", permission.ProductPricingPreview, handle.PreviewPricing)
	g.POST("/pricing/apply", permission.ProductPricingApply, handle.ApplyPricing)
	// 批量改价（商品列表的「按规则改价」抽屉）：作用对象是勾选出来的商品集。
	// 页面时代它曾与「商品更新」共用权限点，现已独立（permission.ProductBulkPricing）——
	// 共用时 Casbin 只会给其中一条路径建策略，另一条对所有人 403。
	g.POST("/bulk-pricing", permission.ProductBulkPricing, handle.BulkPricing)
	g.GET("/pricing/history", permission.ProductPricingHistory, handle.ListPriceAdjustments)
	g.GET("/pricing/adjustment", permission.ProductPricingAdjustment, handle.GetPriceAdjustment)
}

// ══ 页面面装配（/admin 组）══
//
// `/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，handler 在
// product_page.go 及其功能文件里。路由注册只出现在 *_router.go —— 门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面 GET 的 Casbin 待补（见 AGENTS.md §4.3）。

// SetupProductPages 装配 product 域后台页面，返回页面处理器。
// reader 是 products 的读模型面（同一个实现，类型不同）；两者都是构造页处理器的必需实参。
func SetupProductPages(pages *gin.RouterGroup,
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
	// 取词注入：本域页面 handler 里的 `ctx := c.Request.Context()` 因此自带取词函数
	// （service 层的展示文案 —— 内置规则名与描述、筛选标签等 —— 按请求语言渲染）。
	//
	// 用**子组**而不是直接在 pages 上 Use：pages 是装配层创建的共享页面组，
	// 直接 Use 会把中间件挂到其它模块的路由链上。子组前缀留空，路径逐字不变。
	pages = pages.Group("")
	pages.Use(productTranslateMiddleware())
	// 商品管理页（issue #5）：页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作复用商品 API 权限点做 Casbin 鉴权（与既有管理页一致）。
	productPages := NewProductPageHandle(products, reader, projects)
	// 详情页模板选择与预览（issue #14）：模板清单 / 商品发布实例的模板绑定 / 预览渲染。
	productPages.SetDetailTemplateDeps(contentTemplates, presentations)
	// 归属仓下拉（issue #15）：建商品与新增变体时可选仓库（不选即默认仓）。
	productPages.SetInventoryDeps(inventories)
	// 双轨能力（迁移 282）：接口由 presentation 的 contract 声明
	//（presentationcontract.DetailTemplateModePort），且 presentation service 侧有编译期
	// 断言保证它真的实现该端口，故这里直接注入 —— 没有运行期类型断言，也就没有
	// 「断言失败 → 静默降级」这条路径。presentations 的静态类型 PresentationService
	// 已嵌入该端口，接口→接口赋值成立。
	productPages.SetDetailTemplateModePort(presentations)
	// 发布记录页（B9）与发布历史（B11）：同一个 presentation service 的两个收窄读口
	//（PublishRecordPort / PublishAttemptPort 都已嵌入 PresentationService），
	// 经 outbound 翻成「商品 → 逐语言发布明细」与「商品 → 历次尝试」后交给读模型。
	// 两个实参是同一个值，但不合并成一个参数：它们是两条独立能力，
	// 将来若换掉其中一条的提供方，这里就是唯一的改动点。
	productPages.SetPublishRecordDeps(presentations, presentations)
	// 详情页模板的四个写动作（新建 / 首次发布 / 切换 / 改 URL）挂在 /api/product 组，
	// 而那两个契约在 SetupProductRoutes 里还拿不到 —— 同一批依赖在这里后置注入
	//（与文件末尾 BindDetailTemplateModePort 同一个理由，见 product_detail_template_handle.go）。
	BindDetailTemplateWriteDeps(productservice.DetailTemplateDeps{
		Templates: contentTemplates, Instances: presentations, Products: products, Projects: projects,
	})
	// 编辑期 title 唯一性检查的全站数据源（审计 SEO-018）：页面草稿与文章标题
	// 必须和商品域在同一份索引里，否则跨内容的重复标题检不出来。两份契约都可空。
	productPages.SetSeoTitleSources(pageSvc, contentSvc)

	registerProductListPages(pages, productPages)
	registerProductTemplatePages(pages, productPages)
	registerProductAttributePages(pages, productPages)
	registerProductTaxonomyPages(pages, productPages)
	registerProductTagPages(pages, productPages)
	registerProductPricingPages(pages, productPages)
	return productPages
}

// registerProductListPages 注册商品列表 / 新建 / 详情 / 编辑的页面路由。
//
// 页面 GET 走 /admin 组认证（Session + CSRF）并按需 PageAuthz；写动作复用既有 API 权限点。
func registerProductListPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {
	// —— 商品列表 / 新建 / 详情 / 编辑 ——
	pages.GET("/products", adminui.PageAuthz("/api/product/list"), adminui.PageSlot(ProductListPageTemplate), productPages.ProductsPage)
	// 商品新建整页（AGENTS.md §5，弃抽屉）：左表单右模板卡片。
	pages.GET("/products/new", adminui.PageAuthzAs("/api/product/create", http.MethodPost), adminui.PageSlot(ProductNewPageTemplate), productPages.ProductNewPage)
	// 商品详情页：变体与评分是某个商品的子资源，连同四个商品级表单一起从列表页拆出来。
	// 页面 GET 同样走页面组（Session+CSRF，无 Casbin）；页内写动作复用各自既有权限点。
	pages.GET("/products/detail", adminui.PageAuthz("/api/product/list"), adminui.PageSlot(ProductDetailPageTemplate), productPages.ProductDetailPage)
	// 商品基本字段编辑页（名称 / URL 段 / SKU / 状态 / 价格 / 单位 / 重量 / SEO / 图集）
	// 与商品域翻译工作台（多语言）的入口：详情页是子资源维护页，基本字段此前只有创建时能填。
	// 页面 GET 走页面组（Session + CSRF，无 Casbin）；写动作复用商品更新权限点
	//（与 /products/attributes、/products/taxonomy 同一改动面：改的都是这个商品本身）。
	pages.GET("/products/edit", adminui.PageAuthzAs("/api/product/update", http.MethodPost), adminui.PageSlot(ProductEditPageTemplate), productPages.ProductEditPage)
	// 发布记录页（B9）：只读一屏 —— 商品现在发布成什么样（站点路径 / 发布时间 / 产物版本 /
	// 是否上线 / 是否待重建）。**没有历史这一维**：库里没有记录每次发布尝试成功/失败的
	// 明细表，所以这里不编造「上次失败」这类行。页内「重新发布」复用
	// /api/product/detail-template/apply（已有权限点），本页只多一条 GET。
	pages.GET("/products/publish-records", adminui.PageAuthz("/api/product/list"), adminui.PageSlot(ProductPublishRecordPageTemplate), productPages.ProductPublishRecordPage)
	// 变体清单的组合预览（AGENTS.md §8 的「预览—保存」模型）：**只算不写** —— 纯计算，
	// 因此不挂 Casbin（与 /product-attributes/value-rows 同一先例：页面组已有 Session + CSRF，
	// 而这个端点落不了任何库）。返回的行由前端追加进清单，用户点保存才落库。
	// 变体的四个写动作（新建 / 生成 / 删除 / 清单保存）已全部在 /api/product/variant/* 。
	pages.POST("/products/variant/preview", productPages.ProductsVariantPreview)
	// 删除 / 批量删除 / 批量改价 / 挂属性组**都没有页面 POST**：模板用 ui/write-base.js 直调
	// /api/product/{delete,bulk-delete,bulk-pricing,update}。页面组只剩读入口与三个只算不写的
	// 预览端点（变体组合、评分面板、SEO 抽屉）。
	// 双轨写动作（迁移 282）没有页面 POST：模板用 ui/write-base.js 直调
	// /api/product/detail-template/*（见上面接口面那一节）。
	// 产物指针回滚（rollback-artifact）连页面入口一起**删除**：它要求调用方手上有一个
	// 产物 hash，而后台上没有任何地方给出这个事实（模板里从来没渲染过这个表单），
	// 留着就是一条「有路由、无入口」的死端点。
	// 商品评分（issue #33）：评分是独立明细表（#30），增删复用商品更新权限点 ——
	// 评分属商品维护，不另立权限点与菜单。

	// SEO 评分（审计 SEO-016）：商品 / 分类 / 品牌页的编辑期评分，按页型自动选权重档案。
	// **只读计算**（不写库、不写产物、不激活 URL），因此与文章编辑页的评分侧栏一样
	// 不叠加 Casbin 权限点 —— 页面组已有 Session + CSRF；评分是提示性的，不拦保存。
	pages.POST("/products/seo-score", adminui.BareSlot(SeoScoreFragmentTemplate), productPages.ProductScorePanel)
	// SEO 评分抽屉片段（商品 / 分类 / 品牌共用一个）：四处入口（商品编辑页、两个表单页、
	// 两个列表页的行内按钮）统一落到这里。与上面两个 POST 端点**同一取舍** ——
	// 只读计算（不写库、不写产物），页面组已有 Session + CSRF，不叠加 Casbin 权限点；
	// 给一个只读片段单独挂权限点，会让「能看编辑页却打不开评分」这种状态冒出来。
	pages.GET("/products/seo/drawer", adminui.PageSlot(EntitySeoDrawerTemplate), productPages.EntitySeoDrawer)
	// 详情页真实预览帧（iframe 直接 src）：页面组鉴权（Session），理由同上一行 ——
	// 只读渲染、不落库，而 iframe 无法携带 CSRF 头。
	pages.GET("/products/preview-frame", adminui.BareSlot(ProductPreviewNoticeTemplate), productPages.ProductPreviewFrame)
	pages.POST("/product-categories/seo-score", adminui.BareSlot(SeoScoreFragmentTemplate), productPages.ProductCategoryScorePanel)
	pages.POST("/product-brands/seo-score", adminui.BareSlot(SeoScoreFragmentTemplate), productPages.ProductBrandScorePanel)
}

// registerProductTemplatePages 注册详情页模板（issue #14）+ 捆绑配置（issue #20）的页面路由。
//
// 页面 GET 走 /admin 组认证（Session + CSRF）并按需 PageAuthz；写动作复用既有 API 权限点。
func registerProductTemplatePages(pages *gin.RouterGroup, productPages *ProductPageHandle) {
	// —— 详情页模板（issue #14）——
	// 商品详情页模板：一个商品类型下可建多套命名模板，商品发布时可选一套、
	// 发布前可预览（预览只读渲染，不落库不激活）。
	// 页面上只剩两条页面入口：整页 GET 与**预览**（预览开新标签页看渲染字节，本质是只读）。
	// 新建 / 首次发布 / 切换模板 / 改 URL 四个写动作已迁到 /api/product/detail-template/*，
	// 页面前端走 ui/write-base.js。
	pages.GET("/products/template", adminui.PageAuthz("/api/product/get"), adminui.PageSlot(ProductDetailTemplatePageTemplate), productPages.ProductDetailTemplatePage)
	pages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), adminui.PageSlot(ProductDetailTemplatePageTemplate), productPages.ProductDetailTemplatePreview)
}

// registerProductAttributePages 注册属性库（issue #7）的页面路由。
//
// 页面 GET 走 /admin 组认证（Session + CSRF）并按需 PageAuthz；写动作复用既有 API 权限点。
func registerProductAttributePages(pages *gin.RouterGroup, productPages *ProductPageHandle) {
	// —— 属性库（issue #7）——
	// 属性组与属性值可跨商品复用，故独立页面。
	// 值编辑器的增删行走 HTMX（编辑中的行只存在于 DOM，服务端参与归一与去重）。
	pages.GET("/product-attributes", adminui.PageAuthz("/api/product/attribute/list"), adminui.PageSlot(ProductAttributesPageTemplate), productPages.ProductAttributesPage)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	// 值编辑器的行片段：纯表单操作，不落库，故不挂 Casbin（页面组已有 Session + CSRF）。
	pages.POST("/product-attributes/value-rows", adminui.BareSlot(ProductAttributeRowsTemplate), productPages.ProductAttributesValueRows)
}

// registerProductTaxonomyPages 注册分类与品牌（issue #10）的页面路由。
//
// 页面 GET 走 /admin 组认证（Session + CSRF）并按需 PageAuthz；写动作复用既有 API 权限点。
func registerProductTaxonomyPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {
	// —— 分类与品牌（issue #10）——
	// 分类是树（父子层级 / 排序 / slug / SEO 字段），列表一次渲染整棵树，按树根分页 ——
	// 没有「子级懒加载」端点。品牌是独立实体（logo / 描述 / slug / SEO 字段）。
	// 写动作复用商品 API 权限点做 Casbin 鉴权。
	pages.GET("/product-categories", adminui.PageAuthz("/api/product/category/list"), adminui.PageSlot(ProductCategoriesPageTemplate), productPages.ProductCategoriesPage)
	pages.GET("/product-categories/parents", adminui.PageAuthz("/api/product/category/list"), adminui.BareSlot(ProductCategoryParentsTemplate), productPages.ProductCategoryParents)
	// 分类的写动作没有页面 POST：模板用 ui/write-base.js 直调 /api/product/category/*。
	// 页面组只剩两个读入口（列表页与父级候选搜索）。
	pages.GET("/product-brands", adminui.PageAuthz("/api/product/brand/list"), adminui.PageSlot(ProductBrandsPageTemplate), productPages.ProductBrandsPage)
	// 品牌的写动作没有页面 POST：模板用 ui/write-base.js 直调 /api/product/brand/*。
	// 商品挂载分类（多个 + 主分类）与品牌也没有：属于商品更新，走 /api/product/update。
}

// registerProductTagPages 注册标签库（issue #11）的页面路由。
//
// 页面 GET 走 /admin 组认证（Session + CSRF）并按需 PageAuthz；写动作复用既有 API 权限点。
func registerProductTagPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {
	// —— 标签库（issue #11）——
	// 手工标签 + 内置规则的自动标签（规则只接受白名单参数）。
	// 写动作复用商品标签 API 权限点做 Casbin 鉴权；「重算」是显式重算时机之一。
	pages.GET("/product-tags", adminui.PageAuthz("/api/product/tag/list"), adminui.PageSlot(ProductTagsPageTemplate), productPages.ProductTagsPage)
	// 命中商品片段（审计 PERF-02）：标签页首屏不再逐标签内联命中商品，展开时按页取。
	// 只读渲染、不落库，故与同组的 /products/seo-score、/products/variant/preview 同一先例
	// 不叠加 Casbin（页面组已有 Session + CSRF；凭空加权限点反而会造出「有路由无权限点 ⇒
	// 含超管全员 403」那种缺口）。
	pages.GET("/product-tags/hits", adminui.PageAuthz("/api/product/tag/list"), adminui.PageSlot(ProductTagHitsTemplate), productPages.ProductTagHitsFragment)
	// 商品挂手工标签同样走 /api/product/update（自动标签不在这份表单里）。
}

// registerProductPricingPages 注册定价工具（issue #13）的页面路由。
//
// 页面 GET 走 /admin 组认证（Session + CSRF）并按需 PageAuthz；写动作复用既有 API 权限点。
func registerProductPricingPages(pages *gin.RouterGroup, productPages *ProductPageHandle) {
	// —— 定价工具（issue #13）——
	// 四种内置规则 + 尾数处理，对单个 SKU / 单商品全部变体 / 筛选集批量改价。
	// 试算走预览权限点（不落库），应用走应用权限点（落库 + 留痕）。
	pages.GET("/product-pricing", adminui.PageAuthz("/api/product/list"), adminui.PageSlot(ProductPricingPageTemplate), productPages.ProductPricingPage)
	// 试算保留页面 POST：结果是**一张表格**（逐变体的成本 / 原价 / 新价 / 差值 / 结果），
	// 只有整页重渲才渲染得出来；它不落库，没有写动作要迁。应用调价已迁到
	// /api/product/pricing/apply（页面走 ui/write-base.js，落库后整页刷新看台账）。
	pages.POST("/product-pricing/preview", builtin.CasbinMiddlewareForPath("/api/product/pricing/preview"), adminui.PageSlot(ProductPricingPageTemplate), productPages.ProductPricingPreview)

	// —— 商品域翻译工作台（issue #12）——
	// 入口在商品列表行内「多语言」按钮（与页面翻译工作台同构）。路由在装配层同时挂到
	// 页面组与 /api 组（见 assembly_publish 的 SetupProductTranslationRoutes 调用）：
	// 保存已经是接口端点，而页面组与接口组在这两个函数里不可能同时拿到。
}

// SetupProductTranslationRoutes 注册商品域翻译工作台的两条路由：页面组 GET + /api/product 组保存。
//
// 为什么同时要两个组：页面（整页 GET）挂在 /admin 组，而保存已经是接口端点（模板用
// ui/write-base.js 提交），必须挂在 authorizedAPI 的声明式组上才拿得到 Session + CSRF + Casbin
// 三层链。两个组分别由 assembly.go 与 assembly_publish.go 持有，所以这个函数由装配层调用，
// **不**由 SetupProductPages 代劳。
//
// 权限点定死在函数内：参数为 nil 的注入式 guard 会静默注册成「登录即可写」的 fail-open
// 分支，静态门禁也看不见真实取值（审计 SEC-011 的结论）。GET 属安全方法，
// 页面组已有 Session + CSRF，权限点声明走 PageAuthz。
func SetupProductTranslationRoutes(apiRoot *permission.RouteGroup, adminPages *gin.RouterGroup,
	products productcontract.ProductService, projects projectcontract.ProjectService, pages pagecontract.PageService,
	instances ProductTranslationInstancePort) *ProductTranslationHandle {
	handle := NewProductTranslationHandle(products, projects, pages, instances)
	adminPages.GET("/products/translations", adminui.PageAuthz("/api/product/list"), adminui.PageSlot(ProductTranslationsPageTemplate), handle.ProductTranslations)
	// 前缀在这里定死（/api/product），与 SetupProductRoutes 的 g 同形：装配层只递根组，
	// 端点路径不散落到装配层 —— 否则一处写 /translations/save 就成了 /api/translations/save，
	// 权限点与页面写的端点对不上，而两边都不报错。
	apiRoot.Group("/product").POST("/translations/save", permission.ProductTranslationsSave, handle.SaveProductTranslations)
	return handle
}

// ══ 后置注入（装配顺序倒不过来的一条依赖）══

// detailTemplateModePort 双轨能力（迁移 282）在**接口面**的注入点：装配期写一次，之后只读。
//
// 为什么是包级变量而不是构造参数：product 的接口面在 assembly.go 里装配，而 presentation
// （提供双轨能力的那个模块）在 assembly_publish.go 才建出来 —— 顺序反不过来。
// 与 inventoryhttp.SetProductCatalog 同一手法（跨模块循环依赖在装配期的唯一解法）。
//
// 未注入时两个端点回 500 归口文案：装配缺陷必须可见，不能静默成功。真实装配里
// assembly_publish.go 紧跟 presentation 建好之后调 BindDetailTemplateModePort，
// 漏了就是启动期接线错误，而不是运行期的空操作。
var detailTemplateModePort presentationcontract.DetailTemplateModePort

// BindDetailTemplateModePort 绑定双轨能力（装配期调用一次）。
func BindDetailTemplateModePort(port presentationcontract.DetailTemplateModePort) {
	detailTemplateModePort = port
}

// ==========================================================================
// 模块入口：两个处理器类型、构造与装配注入
// ==========================================================================
// 原先单独一个 product_page.go；它只放这两个类型，构造点与路由声明点分开没有收益 ——
// 路由是「谁被暴露」，类型是「谁来处理」，两者是同一张表的两列。

// variantSelectionPrefix 组合生成表单里「属性组 → 勾选值」的字段名前缀。

// 字段名形如 attr:<属性组 id>（多选 checkbox），一个属性组一组值；
// 提交时按前缀收拢成 service 的 selections。
const variantSelectionPrefix = "attr:"

// ProductPageHandle 商品后台页处理器。
type ProductPageHandle struct {
	products productcontract.ProductService
	// reader 读模型（contract.ProductPageReader）：与 products 是**同一个实现的两个静态类型**，
	// 由 SetupProductRoutes 一次产出、装配期一起交进来。
	//
	// 为什么不像以前那样在请求期 h.products.(ProductPageReader) 断言：断言把「装配漏接」
	// 推迟到运营点开那一页时才暴露，只能给 500 兜底，漏接既不会编译报错也不会被门禁抓到；
	// 现在漏接 = 构造处少一个实参 = 编译错误（判据见 product_reader_injection_test.go）。
	reader   productcontract.ProductPageReader
	projects projectcontract.ProjectService
	// templates / instances 供「详情页模板」页（issue #14）消费：模板列表与版本、
	// 商品发布实例的模板绑定与预览渲染。经 SetDetailTemplateDeps 注入 ——
	// 未注入时该页给出装配提示，不影响商品列表页与既有测试的构造签名。
	templates contenttemplatecontract.ContentTemplateService
	instances ProductDetailTemplatePort
	// modePort 双轨能力（迁移 282）：独立文档 / 重新套用预设 / 回滚 / 影响面计数。
	// 单独字段而不是并入 instances：这六条动作属于另一条收窄端口
	//（presentationcontract.DetailTemplateModePort），装配期直接注入（实参
	// presentationSvc 已嵌入该端口，无需断言）；未注入时降级为只有基础面板。
	modePort ProductDetailTemplateModePort
	// inventories → 已随「整页 GET 取数下沉」拆成两条**页面只读端口**（见下面两个字段）：
	// 页面处理器不再直接持有库存 / 内容模板的读写能力，只拿「这一屏要显示哪些选项」。
	//
	// warehouses / entityTemplates 是**还没走读模型的老路径**（编辑页、详情页、捆绑配置页
	// 的抽屉）用的端口：那几屏的取数尚未下沉，这里先共用同一份口径（标签怎么拼、候选取
	// 多少条都在 outbound 适配器里唯一一份），避免页面自己再拼一遍。
	warehouses      productcontract.WarehouseOptionsPort
	entityTemplates productcontract.EntityTemplatePort
	// seoPages / seoContents 编辑期 title 唯一性检查的另外两个数据源（审计 SEO-018）：
	// 页面草稿的 SEO 标题与文章标题都算「同站点已存在的内容」，只比商品域会漏掉
	// 跨内容的重复。经 SetSeoTitleSources 注入 —— 可空，未注入时索引退化为商品域。
	seoPages    pagecontract.PageService
	seoContents contentcontract.ContentService
}

// SetPublishRecordDeps 注入发布记录页的只读端口（B9，装配期调用）。
//
// 实参 presentations 的静态类型 PresentationService 已嵌入 presentation 的
// PublishRecordPort（presentation service 侧有编译期断言），因此这里不需要运行期类型断言。
// 端口经 outbound/publishrecord 翻成「商品 → 逐语言明细」，再交给本模块 service 读模型
// —— 与 SetInventoryDeps / SetDetailTemplateDeps 同一条链路（装配段只调这一个 setter）。
//
// 未注入时那一屏照常 200 并给一句「发布能力未装配」：那是装配缺陷的可见提示，
// 不是「数据读不到」（后者可以重试）。
func (h *ProductPageHandle) SetPublishRecordDeps(instances presentationcontract.PublishRecordPort,
	attempts presentationcontract.PublishAttemptPort) {
	port := publishrecordoutbound.NewWithAttempts(instances, attempts)
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetPublishRecordPort(port)
		wiring.SetPublishAttemptPort(port)
	}
}

// productPagePortWiring 装配期把两条页面只读端口交到**本模块 service 读模型**的接线面。
//
// 为什么由页面处理器转交、而不是装配段直接调 service 的 Set*：这两条依赖的到达点本来就
// 是 SetInventoryDeps / SetDetailTemplateDeps（既有装配与既有页面测试都只调它们），
// 再加一条平行的「装配段直接注入 service」就是同一份接线两处写 —— 漏一处时表现是
// 「新页看不到仓库下拉 / 模板候选」，不报错、门禁也不红。
//
// 不放进 contract：它不是「能力」，而是「装配动作」，只有本模块的页面处理器需要它。
type productPagePortWiring interface {
	SetWarehouseOptionsPort(productcontract.WarehouseOptionsPort)
	SetEntityTemplatePort(productcontract.EntityTemplatePort)
	SetDetailTemplatePort(productcontract.DetailTemplatePort)
	SetPublishRecordPort(productcontract.PublishRecordReader)
	// SetPublishAttemptPort 发布历史（迁移 673）：同一份适配器（publishrecord.Port）
	// 同时实现两个读口 —— 它们读的是同一条产线的两半事实（当前状态 / 历次尝试）。
	SetPublishAttemptPort(productcontract.PublishAttemptReader)
}

// NewProductPageHandle 构造。
//
// reader 必须显式给出（哪怕它与 products 是同一个值）：它承载的是「这一份实现能不能读
// 页面读模型」这个事实，装配期说清楚比请求期断言强 —— 两个参数类型不同，漏给读模型
// 在编译期就报错。
func NewProductPageHandle(products productcontract.ProductService, reader productcontract.ProductPageReader, projects projectcontract.ProjectService) *ProductPageHandle {
	return &ProductPageHandle{products: products, reader: reader, projects: projects}
}

// SetInventoryDeps 注入仓库清单依赖（issue #15，装配期调用）。
//
// 两个去向，同一次注入：① 本处理器的老路径（编辑页 / 详情页 / 捆绑页抽屉）拿走端口；
// ② 交给本模块 service 的读模型（新建页 / 列表页），否则新页会静默少掉仓库下拉。
// 未注入时两处都是「没有仓库」的降级（不是失败）。
func (h *ProductPageHandle) SetInventoryDeps(inventory inventorycontract.InventoryService) {
	port := inventoryoutbound.New(inventory)
	h.warehouses = port
	if wiring, ok := h.products.(productPagePortWiring); ok {
		wiring.SetWarehouseOptionsPort(port)
	}
}

// ==========================================================================
// 接口面处理器（JSON handler 的接收者）。
// 页面面处理器是 ProductPageHandle（见 product_page.go）。
// ==========================================================================
// ProductHandle product 接口处理器。
type ProductHandle struct {
	svc productcontract.ProductService
}

// NewProductHandle 构造。
func NewProductHandle(svc productcontract.ProductService) *ProductHandle {
	return &ProductHandle{svc: svc}
}

// productTranslateMiddleware 让本模块的 service 能按请求语言取词。
//
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），而译文工作台要取**字段名**（商品名 / 描述 / SEO 标题…，见 contract.FieldLabel）——
// 那些 key 与中文兜底住在 enums，这里只负责带上「当前请求语言」。这里把
// shell.TranslateFor(c) 放进 Request 的 Context，handler 里 `ctx := c.Request.Context()`
// 因此自带取词函数 —— 既不必给每个 contract 方法加 tr 参数，也不会漏掉任何一个调用点。
//
// 其余展示文案（状态、规则名、筛选标签、跳过原因）不经过这里：取值进数据，措辞进模板
// 或响应出口。
//
// 未挂本中间件的路径（单测直调 service）行为不变：取词函数缺失时 service 回落中文兜底
// （见 service/product_shared.go 的 translateFrom）。
func productTranslateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		tr := productservice.TranslateFunc(shell.TranslateFor(c))
		c.Request = c.Request.WithContext(productservice.WithTranslate(c.Request.Context(), tr))
		c.Next()
	}
}
