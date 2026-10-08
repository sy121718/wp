package producthttp

// product_page_router.go — product 域后台页面（/admin 组）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 product_page.go / product_handle.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 写动作一律复用商品 API 的权限点（builtin.CasbinMiddlewareForPath(...)，路径逐字未改）；
// 页面 GET 的 Casbin 待补（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/contenttemplate/contract"
	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
)

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
	// 取词注入：本域页面 handler 里的 `ctx := c.Request.Context()` 因此自带取词函数
	// （service 层的展示文案 —— 内置规则名与描述、筛选标签等 —— 按请求语言渲染）。
	//
	// 用**子组**而不是直接在 pages 上 Use：pages 是装配层创建的共享页面组，
	// 直接 Use 会把中间件挂到其它模块的路由链上。子组前缀留空，路径逐字不变。
	pages = pages.Group("")
	pages.Use(productTranslateMiddleware())
	// 商品管理页（issue #5）：页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作复用商品 API 权限点做 Casbin 鉴权（与既有管理页一致）。
	productPages := NewProductPageHandle(products, projects)
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
	// 编辑期 title 唯一性检查的全站数据源（审计 SEO-018）：页面草稿与文章标题
	// 必须和商品域在同一份索引里，否则跨内容的重复标题检不出来。两份契约都可空。
	productPages.SetSeoTitleSources(pageSvc, contentSvc)

	pages.GET("/products", shell.PageAuthz("/api/product/list"), productPages.ProductsPage)
	// 商品新建整页（docs/04-C §5，弃抽屉）：左表单右模板卡片。
	pages.GET("/products/new", shell.PageAuthzAs("/api/product/create", http.MethodPost), productPages.ProductNewPage)
	// 商品详情页：变体与评分是某个商品的子资源，连同四个商品级表单一起从列表页拆出来。
	// 页面 GET 同样走页面组（Session+CSRF，无 Casbin）；页内写动作复用各自既有权限点。
	pages.GET("/products/detail", shell.PageAuthz("/api/product/list"), productPages.ProductDetailPage)
	// 商品基本字段编辑页（名称 / URL 段 / SKU / 状态 / 价格 / 单位 / 重量 / SEO / 图集）
	// 与商品域翻译工作台（多语言）的入口：详情页是子资源维护页，基本字段此前只有创建时能填。
	// 页面 GET 走页面组（Session + CSRF，无 Casbin）；写动作复用商品更新权限点
	//（与 /products/attributes、/products/taxonomy 同一改动面：改的都是这个商品本身）。
	pages.GET("/products/edit", shell.PageAuthzAs("/api/product/update", http.MethodPost), productPages.ProductEditPage)
	pages.POST("/products/update", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsUpdate)
	pages.POST("/products/create", builtin.CasbinMiddlewareForPath("/api/product/create"), productPages.ProductsCreate)
	pages.POST("/products/variant/create", builtin.CasbinMiddlewareForPath("/api/product/variant/create"), productPages.ProductsVariantCreate)
	pages.POST("/products/variant/delete", builtin.CasbinMiddlewareForPath("/api/product/variant/delete"), productPages.ProductsVariantDelete)
	// 变体组合生成（issue #8）：勾选属性值 → 笛卡尔积；不勾选则按全部启用值生成。
	// **落库口径一字未改**（接口 / 导入 / 批量生成仍走它），抽屉的「生成」自本批起走下面的预览。
	pages.POST("/products/variant/generate", builtin.CasbinMiddlewareForPath("/api/product/variant/generate"), productPages.ProductsVariantGenerate)
	// 变体清单的「预览—保存」模型（docs/14 §8）：
	//   · preview —— 只算不写（纯计算，**不挂 Casbin**，与 /product-attributes/value-rows 同一先例：
	//     页面组已有 Session + CSRF，而这个端点落不了任何库）；
	//   · save —— 以清单为准落库（新增 / 改 SKU / 删清单外），复用组合生成的权限点：
	//     改动的都是「这个商品的规格组合」这一件事，不新增权限点。
	pages.POST("/products/variant/preview", productPages.ProductsVariantPreview)
	pages.POST("/products/variant/save", builtin.CasbinMiddlewareForPath("/api/product/variant/generate"), productPages.ProductsVariantSave)
	pages.POST("/products/delete", builtin.CasbinMiddlewareForPath("/api/product/delete"), productPages.ProductsDelete)
	// 批量删除：与单条删除共用同一个权限点与同一条 service 路径，逐条处理、单条失败不整批回滚。
	pages.POST("/products/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/delete"), productPages.ProductsBulkDelete)
	// 批量「按规则改价」（本批）：定价工具的入口收进商品列表的批量操作，作用对象就是
	// 勾选出来的商品集。权限点复用商品更新（同一改动面：改的都是商品变体的售价），
	// 不新增权限点 —— /api/product/update 该路径已有策略，超管与拥有该权限的角色都能用。
	pages.POST("/products/bulk-pricing", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsBulkPricing)
	// 商品引用的属性组整体替换（issue #7）：复用商品更新权限点（同一改动面）。
	pages.POST("/products/attributes", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsAttributesSet)
	// 双轨写动作（迁移 282，docs/04-C-instance-override.md）：只改这一个商品的呈现。
	// 权限复用商品更新（同一改动面：改的都是这个商品详情页的内容），不新增权限点。
	// 成功不写额外回执（页面上的模式徽标就是结果），失败走白名单文案 + 整页提示。
	pages.POST("/products/reapply-preset", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsReapplyPreset)
	pages.POST("/products/rollback-document", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRollbackDocument)
	pages.POST("/products/rollback-artifact", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRollbackArtifact)
	// 商品评分（issue #33）：评分是独立明细表（#30），增删复用商品更新权限点 ——
	// 评分属商品维护，不另立权限点与菜单。
	pages.POST("/products/rating/add", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRatingAdd)
	pages.POST("/products/rating/delete", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsRatingDelete)
	// SEO 评分（审计 SEO-016）：商品 / 分类 / 品牌页的编辑期评分，按页型自动选权重档案。
	// **只读计算**（不写库、不写产物、不激活 URL），因此与文章编辑页的评分侧栏一样
	// 不叠加 Casbin 权限点 —— 页面组已有 Session + CSRF；评分是提示性的，不拦保存。
	pages.POST("/products/seo-score", productPages.ProductScorePanel)
	// SEO 评分抽屉片段（商品 / 分类 / 品牌共用一个）：四处入口（商品编辑页、两个表单页、
	// 两个列表页的行内按钮）统一落到这里。与上面两个 POST 端点**同一取舍** ——
	// 只读计算（不写库、不写产物），页面组已有 Session + CSRF，不叠加 Casbin 权限点；
	// 给一个只读片段单独挂权限点，会让「能看编辑页却打不开评分」这种状态冒出来。
	pages.GET("/products/seo/drawer", productPages.EntitySeoDrawer)
	// 详情页真实预览帧（iframe 直接 src）：页面组鉴权（Session），理由同上一行 ——
	// 只读渲染、不落库，而 iframe 无法携带 CSRF 头。
	pages.GET("/products/preview-frame", productPages.ProductPreviewFrame)
	pages.POST("/product-categories/seo-score", productPages.ProductCategoryScorePanel)
	pages.POST("/product-brands/seo-score", productPages.ProductBrandScorePanel)

	// 商品详情页模板可选与预览（issue #14）：一个商品类型下可建多套命名模板，
	// 商品发布时可选一套、发布前可预览（预览只读渲染，不落库不激活）。
	// 写动作分别复用内容模板创建 / 实例创建 / 实例重建 / 实例预览四个 API 权限点。
	// 捆绑配置（issue #20）：配置页 + 保存（保存复用 /api/product/bundle/set 的权限点）。
	pages.GET("/products/bundle", shell.PageAuthz("/api/product/list"), productPages.ProductBundlePage)
	pages.POST("/products/bundle/save", builtin.CasbinMiddlewareForPath("/api/product/bundle/set"), productPages.ProductBundleSave)
	// 成员来源解析（docs/14 §1.2 的三种来源，本批）：只解析、**不落库** ——
	// 与 /products/variant/preview 同一先例（页面组已有 Session + CSRF，端点落不了任何库），
	// 因此不叠加 Casbin 权限点，也就不会出现「有路由、无权限点」的全员 403。
	pages.POST("/products/bundle/members/resolve", productPages.ProductsBundleMembersResolve)
	pages.GET("/products/template", shell.PageAuthz("/api/product/get"), productPages.ProductDetailTemplatePage)
	pages.POST("/products/template/create", builtin.CasbinMiddlewareForPath("/api/contenttemplate/create"), productPages.ProductDetailTemplateCreate)
	pages.POST("/products/template/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), productPages.ProductDetailTemplatePublish)
	pages.POST("/products/template/apply", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), productPages.ProductDetailTemplateApply)
	pages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), productPages.ProductDetailTemplatePreview)
	// 改 URL：详情页发布后换路径（新路径激活 + 旧路径 301 或取消激活）。
	// 权限点独立于创建/重建：改 URL 会动旧链接的未来行为，是不同影响面的动作。
	pages.POST("/products/template/url", builtin.CasbinMiddlewareForPath("/api/presentation/update-url"), productPages.ProductDetailTemplateUpdateURL)

	// 商品属性管理页（issue #7）：属性组与属性值可跨商品复用，故独立页面。
	// 值编辑器的增删行走 HTMX（编辑中的行只存在于 DOM，服务端参与归一与去重）。
	pages.GET("/product-attributes", shell.PageAuthz("/api/product/attribute/list"), productPages.ProductAttributesPage)
	pages.POST("/product-attributes/create", builtin.CasbinMiddlewareForPath("/api/product/attribute/create"), productPages.ProductAttributesCreate)
	pages.POST("/product-attributes/update", builtin.CasbinMiddlewareForPath("/api/product/attribute/update"), productPages.ProductAttributesUpdate)
	pages.POST("/product-attributes/set-values", builtin.CasbinMiddlewareForPath("/api/product/attribute/set-values"), productPages.ProductAttributesSetValues)
	pages.POST("/product-attributes/delete", builtin.CasbinMiddlewareForPath("/api/product/attribute/delete"), productPages.ProductAttributesDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-attributes/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/attribute/delete"), productPages.ProductAttributesBulkDelete)
	// 值编辑器的行片段：纯表单操作，不落库，故不挂 Casbin（页面组已有 Session + CSRF）。
	pages.POST("/product-attributes/value-rows", productPages.ProductAttributesValueRows)

	// 商品分类与品牌管理页（issue #10）：分类是树（父子层级 / 排序 / slug / SEO 字段），
	// 列表一次渲染整棵树，按树根分页 —— 没有「子级懒加载」端点。
	// 品牌是独立实体（logo / 描述 / slug / SEO 字段）。写动作复用商品 API 权限点做 Casbin 鉴权。
	pages.GET("/product-categories", shell.PageAuthz("/api/product/category/list"), productPages.ProductCategoriesPage)
	pages.GET("/product-categories/parents", shell.PageAuthz("/api/product/category/list"), productPages.ProductCategoryParents)
	pages.POST("/product-categories/create", builtin.CasbinMiddlewareForPath("/api/product/category/create"), productPages.ProductCategoriesCreate)
	pages.POST("/product-categories/update", builtin.CasbinMiddlewareForPath("/api/product/category/update"), productPages.ProductCategoriesUpdate)
	pages.POST("/product-categories/delete", builtin.CasbinMiddlewareForPath("/api/product/category/delete"), productPages.ProductCategoriesDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-categories/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/category/delete"), productPages.ProductCategoriesBulkDelete)
	pages.GET("/product-brands", shell.PageAuthz("/api/product/brand/list"), productPages.ProductBrandsPage)
	pages.POST("/product-brands/create", builtin.CasbinMiddlewareForPath("/api/product/brand/create"), productPages.ProductBrandsCreate)
	pages.POST("/product-brands/update", builtin.CasbinMiddlewareForPath("/api/product/brand/update"), productPages.ProductBrandsUpdate)
	pages.POST("/product-brands/delete", builtin.CasbinMiddlewareForPath("/api/product/brand/delete"), productPages.ProductBrandsDelete)
	// 批量删除复用同一条删除路径与权限点：逐条校验，失败的那条不计入成功数。
	pages.POST("/product-brands/bulk-delete", builtin.CasbinMiddlewareForPath("/api/product/brand/delete"), productPages.ProductBrandsBulkDelete)
	// 商品挂载分类（多个 + 主分类）与品牌：属于商品更新，复用商品更新权限点。
	pages.POST("/products/taxonomy", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsTaxonomySet)

	// 商品标签管理页（issue #11）：手工标签 + 内置规则的自动标签（规则只接受白名单参数）。
	// 写动作复用商品标签 API 权限点做 Casbin 鉴权；「重算」是显式重算时机之一。
	pages.GET("/product-tags", shell.PageAuthz("/api/product/tag/list"), productPages.ProductTagsPage)
	// 命中商品片段（审计 PERF-02）：标签页首屏不再逐标签内联命中商品，展开时按页取。
	// 只读渲染、不落库，故与同组的 /products/seo-score、/products/variant/preview 同一先例
	// 不叠加 Casbin（页面组已有 Session + CSRF；凭空加权限点反而会造出「有路由无权限点 ⇒
	// 含超管全员 403」那种缺口）。
	pages.GET("/product-tags/hits", shell.PageAuthz("/api/product/tag/list"), productPages.ProductTagHitsFragment)
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
	pages.GET("/product-pricing", shell.PageAuthz("/api/product/list"), productPages.ProductPricingPage)
	pages.POST("/product-pricing/preview", builtin.CasbinMiddlewareForPath("/api/product/pricing/preview"), productPages.ProductPricingPreview)
	pages.POST("/product-pricing/apply", builtin.CasbinMiddlewareForPath("/api/product/pricing/apply"), productPages.ProductPricingApply)

	// 商品域翻译工作台（issue #12）：入口在商品列表行内「多语言」按钮（与页面翻译工作台同构）。
	// 保存写 sys_translation（engine=manual）并标记待重建，鉴权由 SetupProductTranslationRoutes
	// 在函数内挂定商品更新权限点（同一改动面）。
	SetupProductTranslationRoutes(pages, products, projects, pageSvc, presentations)
	return productPages
}

// SetupProductTranslationRoutes 注册商品域翻译页路由（挂 /admin 页面组）。
//
// 保存端点的鉴权在函数内挂定：页面组（internal/routers/assembly.go 的 adminPages）
// 只有 Session + CSRF + 权限上下文，没有鉴权判定能力 —— 页面写端点必须各自显式挂
// Casbin 中间件。保存改的是商品的展示文本，复用商品更新权限点（与页面翻译工作台
// 同一口径）；GET 属安全方法，页面组已有 Session + CSRF。
//
// 这里刻意不再接受「由调用方注入 guard」的参数：审计发现参数为 nil 时会静默注册成
// 「登录即可写」的 fail-open 分支，而静态门禁也看不见参数化注入的真实取值。
// 权限点定死在函数内，漏挂无处可藏。
func SetupProductTranslationRoutes(adminPages *gin.RouterGroup,
	products productcontract.ProductService, projects projectcontract.ProjectService, pages pagecontract.PageService,
	instances ProductTranslationInstancePort) *productTranslationHandle {
	handle := NewProductTranslationHandle(products, projects, pages, instances)
	adminPages.GET("/products/translations", shell.PageAuthz("/api/product/list"), handle.ProductTranslations)
	adminPages.POST("/products/translations/save",
		builtin.CasbinMiddlewareForPath("/api/product/update"), handle.SaveProductTranslations)
	return handle
}
