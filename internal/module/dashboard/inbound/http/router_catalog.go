package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_catalog.go - 商品目录页路由：商品/变体/评分、属性、分类与品牌、标签、定价。

func setupCatalogRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 商品管理页（issue #5）：页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作复用商品 API 权限点做 Casbin 鉴权（与既有管理页一致）。
	productPages := NewProductPageHandle(d.products, d.projects)
	// 详情页模板选择与预览（issue #14）：模板清单 / 商品发布实例的模板绑定 / 预览渲染。
	productPages.SetDetailTemplateDeps(d.templates, d.presentations)
	// 归属仓下拉（issue #15）：建商品与新增变体时可选仓库（不选即默认仓）。
	productPages.SetInventoryDeps(d.inventories)
	// 编辑期 title 唯一性检查的全站数据源（审计 SEO-018）：页面草稿与文章标题
	// 必须和商品域在同一份索引里，否则跨内容的重复标题检不出来。两份契约都可空。
	productPages.SetSeoTitleSources(d.pages, d.contents)
	adminPages.GET("/products", productPages.ProductsPage)
	adminPages.POST("/products/create", builtin.CasbinMiddlewareForPath("/api/product/create"), productPages.ProductsCreate)
	adminPages.POST("/products/variant/create", builtin.CasbinMiddlewareForPath("/api/product/variant/create"), productPages.ProductsVariantCreate)
	adminPages.POST("/products/variant/delete", builtin.CasbinMiddlewareForPath("/api/product/variant/delete"), productPages.ProductsVariantDelete)
	// 变体组合生成（issue #8）：勾选属性值 → 笛卡尔积；不勾选则按全部启用值生成。
	adminPages.POST("/products/variant/generate", builtin.CasbinMiddlewareForPath("/api/product/variant/generate"), productPages.ProductsVariantGenerate)
	adminPages.POST("/products/delete", builtin.CasbinMiddlewareForPath("/api/product/delete"), productPages.ProductsDelete)
	// 商品引用的属性组整体替换（issue #7）：复用商品更新权限点（同一改动面）。
	adminPages.POST("/products/attributes", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsAttributesSet)
	// 商品评分（issue #33）：评分是独立明细表（#30），增删复用商品更新权限点 ——
	// 评分属商品维护，不另立权限点与菜单。
	adminPages.POST("/products/rating/add", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRatingAdd)
	adminPages.POST("/products/rating/delete", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRatingDelete)
	// SEO 评分（审计 SEO-016）：商品 / 分类 / 品牌页的编辑期评分，按页型自动选权重档案。
	// **只读计算**（不写库、不写产物、不激活 URL），因此与文章编辑页的评分侧栏一样
	// 不叠加 Casbin 权限点 —— 页面组已有 Session + CSRF；评分是提示性的，不拦保存。
	adminPages.POST("/products/seo-score", productPages.ProductScorePanel)
	adminPages.POST("/product-categories/seo-score", productPages.ProductCategoryScorePanel)
	adminPages.POST("/product-brands/seo-score", productPages.ProductBrandScorePanel)

	// 商品详情页模板可选与预览（issue #14）：一个商品类型下可建多套命名模板，
	// 商品发布时可选一套、发布前可预览（预览只读渲染，不落库不激活）。
	// 写动作分别复用内容模板创建 / 实例创建 / 实例重建 / 实例预览四个 API 权限点。
	// 捆绑配置（issue #20）：配置页 + 保存（保存复用 /api/product/bundle/set 的权限点）。
	adminPages.GET("/products/bundle", productPages.ProductBundlePage)
	adminPages.POST("/products/bundle/save", builtin.CasbinMiddlewareForPath("/api/product/bundle/set"), productPages.ProductBundleSave)
	adminPages.GET("/products/template", productPages.ProductDetailTemplatePage)
	adminPages.POST("/products/template/create", builtin.CasbinMiddlewareForPath("/api/contenttemplate/create"), productPages.ProductDetailTemplateCreate)
	adminPages.POST("/products/template/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), productPages.ProductDetailTemplatePublish)
	adminPages.POST("/products/template/apply", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), productPages.ProductDetailTemplateApply)
	adminPages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), productPages.ProductDetailTemplatePreview)
	// 改 URL：详情页发布后换路径（新路径激活 + 旧路径 301 或取消激活）。
	// 权限点独立于创建/重建：改 URL 会动旧链接的未来行为，是不同影响面的动作。
	adminPages.POST("/products/template/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), productPages.ProductDetailTemplateUpdateURL)

	// 商品属性管理页（issue #7）：属性组与属性值可跨商品复用，故独立页面。
	// 值编辑器的增删行走 HTMX（编辑中的行只存在于 DOM，服务端参与归一与去重）。
	adminPages.GET("/product-attributes", productPages.ProductAttributesPage)
	adminPages.POST("/product-attributes/create", builtin.CasbinMiddlewareForPath("/api/product/attribute/create"), productPages.ProductAttributesCreate)
	adminPages.POST("/product-attributes/update", builtin.CasbinMiddlewareForPath("/api/product/attribute/update"), productPages.ProductAttributesUpdate)
	adminPages.POST("/product-attributes/set-values", builtin.CasbinMiddlewareForPath("/api/product/attribute/set-values"), productPages.ProductAttributesSetValues)
	adminPages.POST("/product-attributes/delete", builtin.CasbinMiddlewareForPath("/api/product/attribute/delete"), productPages.ProductAttributesDelete)
	// 值编辑器的行片段：纯表单操作，不落库，故不挂 Casbin（页面组已有 Session + CSRF）。
	adminPages.POST("/product-attributes/value-rows", productPages.ProductAttributesValueRows)

	// 商品分类与品牌管理页（issue #10）：分类是树（父子层级 / 排序 / slug / SEO 字段），
	// 品牌是独立实体（logo / 描述 / slug / SEO 字段）。写动作复用商品 API 权限点做 Casbin 鉴权。
	adminPages.GET("/product-categories", productPages.ProductCategoriesPage)
	adminPages.POST("/product-categories/create", builtin.CasbinMiddlewareForPath("/api/product/category/create"), productPages.ProductCategoriesCreate)
	adminPages.POST("/product-categories/update", builtin.CasbinMiddlewareForPath("/api/product/category/update"), productPages.ProductCategoriesUpdate)
	adminPages.POST("/product-categories/delete", builtin.CasbinMiddlewareForPath("/api/product/category/delete"), productPages.ProductCategoriesDelete)
	adminPages.GET("/product-brands", productPages.ProductBrandsPage)
	adminPages.POST("/product-brands/create", builtin.CasbinMiddlewareForPath("/api/product/brand/create"), productPages.ProductBrandsCreate)
	adminPages.POST("/product-brands/update", builtin.CasbinMiddlewareForPath("/api/product/brand/update"), productPages.ProductBrandsUpdate)
	adminPages.POST("/product-brands/delete", builtin.CasbinMiddlewareForPath("/api/product/brand/delete"), productPages.ProductBrandsDelete)
	// 商品挂载分类（多个 + 主分类）与品牌：属于商品更新，复用商品更新权限点。
	adminPages.POST("/products/taxonomy", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsTaxonomySet)

	// 商品标签管理页（issue #11）：手工标签 + 内置规则的自动标签（规则只接受白名单参数）。
	// 写动作复用商品标签 API 权限点做 Casbin 鉴权；「重算」是显式重算时机之一。
	adminPages.GET("/product-tags", productPages.ProductTagsPage)
	adminPages.POST("/product-tags/create", builtin.CasbinMiddlewareForPath("/api/product/tag/create"), productPages.ProductTagsCreate)
	adminPages.POST("/product-tags/update", builtin.CasbinMiddlewareForPath("/api/product/tag/update"), productPages.ProductTagsUpdate)
	adminPages.POST("/product-tags/delete", builtin.CasbinMiddlewareForPath("/api/product/tag/delete"), productPages.ProductTagsDelete)
	adminPages.POST("/product-tags/recalc", builtin.CasbinMiddlewareForPath("/api/product/tag/recalc"), productPages.ProductTagsRecalc)
	// 商品挂手工标签：属于商品更新，复用商品更新权限点（自动标签不在这份表单里）。
	adminPages.POST("/products/tags", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsTagsSet)

	// 定价工具（issue #13）：四种内置规则 + 尾数处理，对单个 SKU / 单商品全部变体 / 筛选集
	// 批量改价。试算走预览权限点（不落库），应用走应用权限点（落库 + 留痕）。
	adminPages.GET("/product-pricing", productPages.ProductPricingPage)
	adminPages.POST("/product-pricing/preview", builtin.CasbinMiddlewareForPath("/api/product/pricing/preview"), productPages.ProductPricingPreview)
	adminPages.POST("/product-pricing/apply", builtin.CasbinMiddlewareForPath("/api/product/pricing/apply"), productPages.ProductPricingApply)
}
