// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"go_wp/internal/builder/core"

	admincontract "go_wp/internal/module/admin/contract"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	inventorycontract "go_wp/internal/module/inventory/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/gin-gonic/gin"
)

// SetupDashboardRoutes 注册后台页面路由（挂载到引擎根路径）。
//
// admin 六领域 CRUD 契约（管理员/角色/菜单/权限/部门/数据权限）注入给 admin 管理页消费；
// 同一合并 Service 同时实现全部接口，由 routes.go 装配时以同一 svc 传入。
// authz 提供当前用户有效权限码，页面渲染层据此过滤菜单/按钮/字段（PermSet）。
func SetupDashboardRoutes(router *gin.Engine,
	pages pagecontract.PageService,
	projects projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	plugins plugincontract.PluginService,
	collection core.CollectionResolver,
	admins admincontract.AdminService,
	roles admincontract.RoleService,
	perms admincontract.PermService,
	menus admincontract.MenuService,
	depts admincontract.DeptService,
	rules admincontract.RuleService,
	authz admincontract.AuthzContextService,
	navigations navigationcontract.NavigationService,
	products productcontract.ProductService,
	presentations ProductPagePorts,
	templates contenttemplatecontract.ContentTemplateService,
	inventories inventorycontract.InventoryService) {
	if router == nil {
		return
	}

	handle := NewHandle(pages, projects, blocks, plugins, collection, admins, roles, perms, menus, depts, rules, authz, navigations)

	// 登录页：不挂认证（未登录请求被中间件 302 到此，独立布局渲染登录表单）。
	router.GET("/admin/login", handle.LoginPage)

	// 页面路由（全部挂 Session 认证：未登录的页面请求由中间件 302 到 /admin/login）。
	// 组级再挂 CSRFMiddleware：GET 直接放行，仅保护 POST /workbench/preview（草稿预览渲染）。
	// 前端刷新画布用原生表单 POST 提交（workbench.js refreshCanvas），已带 csrf_token 隐藏域。
	authPages := router.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), permContextMiddleware(authz))
	authPages.GET("/", handle.Dashboard)
	authPages.GET("/workbench", handle.Workbench)
	authPages.GET("/workbench/preview", handle.Preview)
	authPages.POST("/workbench/preview", handle.PreviewDraft)
	// 检查器面板片段（HTMX 化打样，docs/09 §3）：schema → 表单 HTML 由服务端渲染。
	authPages.POST("/workbench/inspector", handle.InspectorPanel)
	// 结构树片段（HTMX 化）：树 HTML 由服务端渲染，客户端只做一次事件委托。
	authPages.POST("/workbench/outline", handle.OutlineTree)
	// 页面设置面板与评分区（HTMX 化）：表单与评分均由服务端渲染。
	authPages.POST("/workbench/settings", handle.SettingsPanel)
	authPages.POST("/workbench/seo-score-panel", handle.SeoScorePanel)
	// 全局设置面板（站点主题字段）：字段表与渲染由服务端提供。
	authPages.POST("/workbench/global", handle.GlobalPanel)
	// 修订历史列表与恢复（HTMX 化）：列表由服务端渲染，恢复走服务端覆盖保存。
	authPages.POST("/workbench/history", handle.HistoryPanel)
	// 恢复修订会覆盖页面草稿（属写操作），必须做 Casbin 鉴权：
	// 权限点复用「保存草稿」（与 /admin/page/translations/save 同源），
	// 否则任何仅登录后台的低权限用户都能覆盖任意页面草稿。
	authPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), handle.HistoryRestore)
	// SEO 评分：只读分析草稿，返回评分与逐项建议。
	authPages.POST("/workbench/seo-score", handle.SEOScore)
	// 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
	authPages.GET("/workbench/block/preview", handle.BlockPreview)

	// /admin/* 后台页面统一挂 Session 认证 + CSRF 校验。
	// 页面内原生 POST 表单已注入 csrf_token 隐藏域（模板），JS fetch 请求统一带 X-CSRF-Token 头。
	adminPages := router.Group("/admin", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), permContextMiddleware(authz))
	adminPages.GET("", handle.Dashboard)
	// 语言切换（多语言 P1）：校验 lang → 写 Cookie → 302 回跳。
	// 挂 admin 页面组（Session + CSRF，不走 Casbin）：GET 属安全方法，CSRF 直接放行；
	// 无 Handle 依赖，故为包级 handler。
	adminPages.GET("/lang", LangSwitch)
	// 页面管理列表：列出/新建站点工程与页面。
	// 页面写操作复用对应 API 权限点做 Casbin 鉴权（页面路径与权限点路径不一致，
	// 直接以页面路径 enforce 会因权限点表无此路径而拒绝所有用户）。
	adminPages.GET("/pages", handle.PagesList)
	adminPages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), handle.CreatePage)
	// 翻译工作台（多语言 P5c，docs/06-D §7.8）：入口在页面列表行内「多语言」按钮，不做独立菜单。
	// 保存写 sys_translation（engine=manual）并触发全站标记待重建，鉴权复用「保存草稿」权限点。
	adminPages.GET("/page/translations", handle.PageTranslations)
	adminPages.POST("/page/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), handle.SavePageTranslations)
	adminPages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), handle.CreateProject)
	// 全局块管理：页眉/页脚/区块（编辑进工作台；stale 传播在本模块编排）。
	// 前台导航菜单（公开站点导航，与后台权限菜单严格隔离）：结构树 + 排序 + 打开方式。
	adminPages.GET("/navigations", handle.NavigationsPage)
	adminPages.POST("/navigations/create", builtin.CasbinMiddlewareForPath("/api/navigation/create"), handle.NavigationCreate)
	adminPages.POST("/navigations/add-source", builtin.CasbinMiddlewareForPath("/api/navigation/create"), handle.NavigationAddSource)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), handle.NavigationUpdate)
	adminPages.POST("/navigations/delete", builtin.CasbinMiddlewareForPath("/api/navigation/delete"), handle.NavigationDelete)
	adminPages.POST("/navigations/move", builtin.CasbinMiddlewareForPath("/api/navigation/update"), handle.NavigationMove)
	adminPages.GET("/blocks", handle.BlocksList)
	adminPages.POST("/blocks/create", builtin.CasbinMiddlewareForPath("/api/block/create"), handle.CreateBlock)
	adminPages.POST("/blocks/delete", builtin.CasbinMiddlewareForPath("/api/block/delete"), handle.DeleteBlock)
	// 工作台保存块内容（保存后编排 stale 传播）。
	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), handle.SaveBlockContent)
	// 媒体库（左树右库：分类树筛选 + WP 式网格/列表 + 详情编辑）。
	adminPages.GET("/media", handle.MediaPage)
	// 主题管理（多主题：列表/新建/激活/删除 + 单主题设置）。
	adminPages.GET("/themes", handle.ThemeManage)
	adminPages.POST("/themes/create", builtin.CasbinMiddlewareForPath("/api/theme/create"), handle.CreateTheme)
	adminPages.POST("/themes/activate", builtin.CasbinMiddlewareForPath("/api/theme/activate"), handle.ActivateTheme)
	adminPages.POST("/themes/delete", builtin.CasbinMiddlewareForPath("/api/theme/delete"), handle.DeleteTheme)
	adminPages.GET("/themes/settings", handle.ThemeSettings)
	adminPages.POST("/themes/settings/save", builtin.CasbinMiddlewareForPath("/api/theme/update"), handle.SaveThemeSettings)
	// 旧单主题设置入口 → 新主题管理页。
	adminPages.GET("/theme", handle.ThemeRedirect)
	// 站点设置（基础站点信息：站点名/简介/联系邮箱，走 project SiteSettings）。
	adminPages.GET("/settings", handle.SiteSettings)
	adminPages.POST("/settings/save", builtin.CasbinMiddlewareForPath("/api/project/update"), handle.SaveSiteSettings)
	// 站点语言清单（多语言 P3）：行片段走 HTMX 服务端渲染，保存复用 project 更新权限点。
	adminPages.POST("/settings/locales/rows", builtin.CasbinMiddlewareForPath("/api/project/update"), handle.LocaleRowsFragment)
	adminPages.POST("/settings/locales/save", builtin.CasbinMiddlewareForPath("/api/project/update"), handle.SaveSiteLocales)
	// 插件管理（列表/上传安装/启停/卸载，docs/06-plugin-system.md）。
	adminPages.GET("/plugins", handle.PluginsPage)
	adminPages.POST("/plugins/install", builtin.CasbinMiddlewareForPath("/api/plugin/install"), handle.PluginsInstall)
	adminPages.POST("/plugins/toggle", builtin.CasbinMiddlewareForPath("/api/plugin/toggle"), handle.PluginsToggle)
	adminPages.POST("/plugins/uninstall", builtin.CasbinMiddlewareForPath("/api/plugin/uninstall"), handle.PluginsUninstall)

	// 商品管理页（issue #5）：页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作复用商品 API 权限点做 Casbin 鉴权（与既有管理页一致）。
	productPages := NewProductPageHandle(products, projects)
	// 详情页模板选择与预览（issue #14）：模板清单 / 商品发布实例的模板绑定 / 预览渲染。
	productPages.SetDetailTemplateDeps(templates, presentations)
	// 归属仓下拉（issue #15）：建商品与新增变体时可选仓库（不选即默认仓）。
	productPages.SetInventoryDeps(inventories)
	adminPages.GET("/products", productPages.ProductsPage)
	adminPages.POST("/products/create", builtin.CasbinMiddlewareForPath("/api/product/create"), productPages.ProductsCreate)
	adminPages.POST("/products/variant/create", builtin.CasbinMiddlewareForPath("/api/product/variant/create"), productPages.ProductsVariantCreate)
	adminPages.POST("/products/variant/delete", builtin.CasbinMiddlewareForPath("/api/product/variant/delete"), productPages.ProductsVariantDelete)
	// 变体组合生成（issue #8）：勾选属性值 → 笛卡尔积；不勾选则按全部启用值生成。
	adminPages.POST("/products/variant/generate", builtin.CasbinMiddlewareForPath("/api/product/variant/generate"), productPages.ProductsVariantGenerate)
	adminPages.POST("/products/delete", builtin.CasbinMiddlewareForPath("/api/product/delete"), productPages.ProductsDelete)
	// 商品引用的属性组整体替换（issue #7）：复用商品更新权限点（同一改动面）。
	adminPages.POST("/products/attributes", builtin.CasbinMiddlewareForPath("/api/product/update"), productPages.ProductsAttributesSet)

	// 商品详情页模板可选与预览（issue #14）：一个商品类型下可建多套命名模板，
	// 商品发布时可选一套、发布前可预览（预览只读渲染，不落库不激活）。
	// 写动作分别复用内容模板创建 / 实例创建 / 实例重建 / 实例预览四个 API 权限点。
	adminPages.GET("/products/template", productPages.ProductDetailTemplatePage)
	adminPages.POST("/products/template/create", builtin.CasbinMiddlewareForPath("/api/contenttemplate/create"), productPages.ProductDetailTemplateCreate)
	adminPages.POST("/products/template/publish", builtin.CasbinMiddlewareForPath("/api/presentation/create"), productPages.ProductDetailTemplatePublish)
	adminPages.POST("/products/template/apply", builtin.CasbinMiddlewareForPath("/api/presentation/rebuild"), productPages.ProductDetailTemplateApply)
	adminPages.POST("/products/template/preview", builtin.CasbinMiddlewareForPath("/api/presentation/preview"), productPages.ProductDetailTemplatePreview)

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

	// 库存管理页（issue #15）：仓库实体（短码 / 名称 / 状态 / 默认仓）与「某 SKU 的各仓库存」。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用仓库 API 权限点。
	// 库存读的是仓库模块真源，与商品页的「库存缓存」列是两回事。
	inventoryPages := NewInventoryPageHandle(inventories, projects, products)
	adminPages.GET("/inventory", inventoryPages.InventoryPage)
	adminPages.POST("/inventory/warehouse/create", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/create"), inventoryPages.InventoryWarehouseCreate)
	adminPages.POST("/inventory/warehouse/update", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseUpdate)
	adminPages.POST("/inventory/warehouse/default", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/update"), inventoryPages.InventoryWarehouseDefault)
	adminPages.POST("/inventory/warehouse/delete", builtin.CasbinMiddlewareForPath("/api/inventory/warehouse/delete"), inventoryPages.InventoryWarehouseDelete)

	// 商品域翻译工作台（issue #12）：入口在商品列表行内「多语言」按钮（与页面翻译工作台同构）。
	// 保存写 sys_translation（engine=manual）并标记待重建，鉴权复用商品更新权限点（同一改动面）。
	SetupProductTranslationRoutes(adminPages,
		builtin.CasbinMiddlewareForPath("/api/product/update"), products, projects, pages, presentations)

	// admin 六领域管理页（管理员/角色/菜单/权限/部门/数据权限）：
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；
	// 写动作（create/update/delete）挂对应业务 API 权限点做 Casbin 鉴权（与现有页面一致）。
	// 只用 GET/POST，无 RESTful 路径参数。
	adminPages.GET("/administrators", handle.AdministratorsPage)
	adminPages.POST("/administrators/create", builtin.CasbinMiddlewareForPath("/api/admin/create"), handle.AdministratorsCreate)
	adminPages.POST("/administrators/update", builtin.CasbinMiddlewareForPath("/api/admin/edit"), handle.AdministratorsUpdate)
	adminPages.POST("/administrators/delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsDelete)

	adminPages.GET("/roles", handle.RolesPage)
	adminPages.POST("/roles/create", builtin.CasbinMiddlewareForPath("/api/role/create"), handle.RolesCreate)
	adminPages.POST("/roles/update", builtin.CasbinMiddlewareForPath("/api/role/update"), handle.RolesUpdate)
	adminPages.POST("/roles/delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesDelete)

	adminPages.GET("/menus", handle.MenusPage)
	adminPages.POST("/menus/create", builtin.CasbinMiddlewareForPath("/api/menu/create"), handle.MenusCreate)
	adminPages.POST("/menus/update", builtin.CasbinMiddlewareForPath("/api/menu/update"), handle.MenusUpdate)
	adminPages.POST("/menus/delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusDelete)

	adminPages.GET("/permissions", handle.PermissionsPage)
	adminPages.POST("/permissions/create", builtin.CasbinMiddlewareForPath("/api/permission/create"), handle.PermissionsCreate)
	adminPages.POST("/permissions/update", builtin.CasbinMiddlewareForPath("/api/permission/update"), handle.PermissionsUpdate)
	adminPages.POST("/permissions/delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsDelete)

	adminPages.GET("/departments", handle.DepartmentsPage)
	adminPages.POST("/departments/create", builtin.CasbinMiddlewareForPath("/api/dept/create"), handle.DepartmentsCreate)
	adminPages.POST("/departments/update", builtin.CasbinMiddlewareForPath("/api/dept/update"), handle.DepartmentsUpdate)
	adminPages.POST("/departments/delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsDelete)

	adminPages.GET("/datarules", handle.DatarulesPage)
	adminPages.GET("/datarules/edit", handle.DatarulesEditPage)
	adminPages.POST("/datarules/create", builtin.CasbinMiddlewareForPath("/api/datarule/create"), handle.DatarulesCreate)
	adminPages.POST("/datarules/update", builtin.CasbinMiddlewareForPath("/api/datarule/update"), handle.DatarulesUpdate)
	adminPages.POST("/datarules/delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesDelete)
}
