// product_page_router.go — 商品域后台页面路由（原 dashboard/inbound/http/router_catalog.go）。
//
// 页面组由装配层创建（Session 认证 + CSRF + 权限上下文），模块只往上面挂路由：
// 页面 GET 走页面组（无 Casbin），写动作**原样复用商品 API 的权限点**
// （builtin.CasbinMiddlewareForPath(...)，权限点路径逐字未改）。
package producthttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// setupProductPages 注册商品域后台页面：商品 / 变体 / 评分、属性、分类与品牌、标签、
// 定价、捆绑、详情页模板、翻译工作台与 SEO 评分面板。
//
// 依赖与页面一一对应（都只读其他模块的 contract，不碰它们的 service/model）：
//   - contentTemplates / presentations：详情页模板页（清单 / 绑定 / 预览 / 发布 / 改 URL）
//     与翻译保存后的实例 stale 标记；
//   - inventories：建商品与新增变体的「归属仓」下拉；
//   - pageSvc / contentSvc：编辑期 title 唯一性检查的另外两个数据源，同时 pageSvc
//     还供译文保存后标记手工页面待重建。
func SetupProductPages(pages *gin.RouterGroup,
	products productcontract.ProductService,
	projects projectcontract.ProjectService,
	contentTemplates contenttemplatecontract.ContentTemplateService,
	presentations presentationcontract.PresentationService,
	inventories inventorycontract.InventoryService,
	pageSvc pagecontract.PageService,
	contentSvc contentcontract.ContentService) *productPageHandle {
	if pages == nil {
		return nil
	}
	// 商品管理页（issue #5）：页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作复用商品 API 权限点做 Casbin 鉴权（与既有管理页一致）。
	productPages := NewProductPageHandle(products, projects)
	// 详情页模板选择与预览（issue #14）：模板清单 / 商品发布实例的模板绑定 / 预览渲染。
	productPages.SetDetailTemplateDeps(contentTemplates, presentations)
	// 归属仓下拉（issue #15）：建商品与新增变体时可选仓库（不选即默认仓）。
	productPages.SetInventoryDeps(inventories)
	// 编辑期 title 唯一性检查的全站数据源（审计 SEO-018）：页面草稿与文章标题
	// 必须和商品域在同一份索引里，否则跨内容的重复标题检不出来。两份契约都可空。
	productPages.SetSeoTitleSources(pageSvc, contentSvc)

	pages.GET("/products", productPages.ProductsPage)
	// 商品详情页：变体与评分是某个商品的子资源，连同四个商品级表单一起从列表页拆出来。
	// 页面 GET 同样走页面组（Session+CSRF，无 Casbin）；页内写动作复用各自既有权限点。
	pages.GET("/products/detail", productPages.ProductDetailPage)
	pages.POST("/products/create", builtin.CasbinMiddlewareForPath("/api/product/create"), productPages.ProductsCreate)
	pages.POST("/products/variant/create", builtin.CasbinMiddlewareForPath("/api/product/variant/create"), productPages.ProductsVariantCreate)
	pages.POST("/products/variant/delete", builtin.CasbinMiddlewareForPath("/api/product/variant/delete"), productPages.ProductsVariantDelete)
	// 变体组合生成（issue #8）：勾选属性值 → 笛卡尔积；不勾选则按全部启用值生成。
	pages.POST("/products/variant/generate", builtin.CasbinMiddlewareForPath("/api/product/variant/generate"), productPages.ProductsVariantGenerate)
	pages.POST("/products/delete", builtin.CasbinMiddlewareForPath("/api/product/delete"), productPages.ProductsDelete)
	// 批量删除：与单条删除共用同一个权限点与同一条 service 路径，逐条处理、单条失败不整批回滚。
	pages.POST("/products/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/delete"), productPages.ProductsBulkDelete)
	// 商品引用的属性组整体替换（issue #7）：复用商品更新权限点（同一改动面）。
	pages.POST("/products/attributes", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsAttributesSet)
	// 商品评分（issue #33）：评分是独立明细表（#30），增删复用商品更新权限点 ——
	// 评分属商品维护，不另立权限点与菜单。
	pages.POST("/products/rating/add", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRatingAdd)
	pages.POST("/products/rating/delete", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRatingDelete)
	// SEO 评分（审计 SEO-016）：商品 / 分类 / 品牌页的编辑期评分，按页型自动选权重档案。
	// **只读计算**（不写库、不写产物、不激活 URL），因此与文章编辑页的评分侧栏一样
	// 不叠加 Casbin 权限点 —— 页面组已有 Session + CSRF；评分是提示性的，不拦保存。
	pages.POST("/products/seo-score", productPages.ProductScorePanel)
	pages.POST("/product-categories/seo-score", productPages.ProductCategoryScorePanel)
	pages.POST("/product-brands/seo-score", productPages.ProductBrandScorePanel)

	// 商品详情页模板可选与预览（issue #14）：一个商品类型下可建多套命名模板，
	// 商品发布时可选一套、发布前可预览（预览只读渲染，不落库不激活）。
	// 写动作分别复用内容模板创建 / 实例创建 / 实例重建 / 实例预览四个 API 权限点。
	// 捆绑配置（issue #20）：配置页 + 保存（保存复用 /api/product/bundle/set 的权限点）。
	pages.GET("/products/bundle", productPages.ProductBundlePage)
	pages.POST("/products/bundle/save", builtin.CasbinMiddlewareForPath("/api/product/bundle/set"), productPages.ProductBundleSave)
	pages.GET("/products/template", productPages.ProductDetailTemplatePage)
	pages.POST("/products/template/create", builtin.CasbinMiddlewareForPath("/api/contenttemplate/create"), productPages.ProductDetailTemplateCreate)
	pages.POST("/products/template/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), productPages.ProductDetailTemplatePublish)
	pages.POST("/products/template/apply", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), productPages.ProductDetailTemplateApply)
	pages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), productPages.ProductDetailTemplatePreview)
	// 改 URL：详情页发布后换路径（新路径激活 + 旧路径 301 或取消激活）。
	// 权限点独立于创建/重建：改 URL 会动旧链接的未来行为，是不同影响面的动作。
	pages.POST("/products/template/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), productPages.ProductDetailTemplateUpdateURL)

	// 商品属性管理页（issue #7）：属性组与属性值可跨商品复用，故独立页面。
	// 值编辑器的增删行走 HTMX（编辑中的行只存在于 DOM，服务端参与归一与去重）。
	pages.GET("/product-attributes", productPages.ProductAttributesPage)
	pages.POST("/product-attributes/create", builtin.CasbinMiddlewareForPath("/api/product/attribute/create"), productPages.ProductAttributesCreate)
	pages.POST("/product-attributes/update", builtin.CasbinMiddlewareForPath("/api/product/attribute/update"), productPages.ProductAttributesUpdate)
	pages.POST("/product-attributes/set-values", builtin.CasbinMiddlewareForPath("/api/product/attribute/set-values"), productPages.ProductAttributesSetValues)
	pages.POST("/product-attributes/delete", builtin.CasbinMiddlewareForPath("/api/product/attribute/delete"), productPages.ProductAttributesDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-attributes/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/attribute/delete"), productPages.ProductAttributesBulkDelete)
	// 值编辑器的行片段：纯表单操作，不落库，故不挂 Casbin（页面组已有 Session + CSRF）。
	pages.POST("/product-attributes/value-rows", productPages.ProductAttributesValueRows)

	// 商品分类与品牌管理页（issue #10）：分类是树（父子层级 / 排序 / slug / SEO 字段），
	// 品牌是独立实体（logo / 描述 / slug / SEO 字段）。写动作复用商品 API 权限点做 Casbin 鉴权。
	pages.GET("/product-categories", productPages.ProductCategoriesPage)
	pages.POST("/product-categories/create", builtin.CasbinMiddlewareForPath("/api/product/category/create"), productPages.ProductCategoriesCreate)
	pages.POST("/product-categories/update", builtin.CasbinMiddlewareForPath("/api/product/category/update"), productPages.ProductCategoriesUpdate)
	pages.POST("/product-categories/delete", builtin.CasbinMiddlewareForPath("/api/product/category/delete"), productPages.ProductCategoriesDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-categories/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/category/delete"), productPages.ProductCategoriesBulkDelete)
	pages.GET("/product-brands", productPages.ProductBrandsPage)
	pages.POST("/product-brands/create", builtin.CasbinMiddlewareForPath("/api/product/brand/create"), productPages.ProductBrandsCreate)
	pages.POST("/product-brands/update", builtin.CasbinMiddlewareForPath("/api/product/brand/update"), productPages.ProductBrandsUpdate)
	pages.POST("/product-brands/delete", builtin.CasbinMiddlewareForPath("/api/product/brand/delete"), productPages.ProductBrandsDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-brands/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/brand/delete"), productPages.ProductBrandsBulkDelete)
	// 商品挂载分类（多个 + 主分类）与品牌：属于商品更新，复用商品更新权限点。
	pages.POST("/products/taxonomy", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsTaxonomySet)

	// 商品标签管理页（issue #11）：手工标签 + 内置规则的自动标签（规则只接受白名单参数）。
	// 写动作复用商品标签 API 权限点做 Casbin 鉴权；「重算」是显式重算时机之一。
	pages.GET("/product-tags", productPages.ProductTagsPage)
	pages.POST("/product-tags/create", builtin.CasbinMiddlewareForPath("/api/product/tag/create"), productPages.ProductTagsCreate)
	pages.POST("/product-tags/update", builtin.CasbinMiddlewareForPath("/api/product/tag/update"), productPages.ProductTagsUpdate)
	pages.POST("/product-tags/delete", builtin.CasbinMiddlewareForPath("/api/product/tag/delete"), productPages.ProductTagsDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-tags/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/tag/delete"), productPages.ProductTagsBulkDelete)
	pages.POST("/product-tags/recalc", builtin.CasbinMiddlewareForPath("/api/product/tag/recalc"), productPages.ProductTagsRecalc)
	// 商品挂手工标签：属于商品更新，复用商品更新权限点（自动标签不在这份表单里）。
	pages.POST("/products/tags", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsTagsSet)

	// 定价工具（issue #13）：四种内置规则 + 尾数处理，对单个 SKU / 单商品全部变体 / 筛选集
	// 批量改价。试算走预览权限点（不落库），应用走应用权限点（落库 + 留痕）。
	pages.GET("/product-pricing", productPages.ProductPricingPage)
	pages.POST("/product-pricing/preview", builtin.CasbinMiddlewareForPath("/api/product/pricing/preview"), productPages.ProductPricingPreview)
	pages.POST("/product-pricing/apply", builtin.CasbinMiddlewareForPath("/api/product/pricing/apply"), productPages.ProductPricingApply)

	// 商品域翻译工作台（issue #12）：入口在商品列表行内「多语言」按钮（与页面翻译工作台同构）。
	// 保存写 sys_translation（engine=manual）并标记待重建，鉴权复用商品更新权限点（同一改动面）。
	SetupProductTranslationRoutes(pages,
		builtin.CasbinMiddlewareForPath("/api/product/update"), products, projects, pageSvc, presentations)
	return productPages
}
