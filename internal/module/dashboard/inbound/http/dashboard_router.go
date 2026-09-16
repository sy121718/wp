// Package dashboardhttp 注册 dashboard 模块的页面路由。
package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"go_wp/internal/builder/core"

	admincontract "go_wp/internal/module/admin/contract"
	analyticscontract "go_wp/internal/module/analytics/contract"
	blockcontract "go_wp/internal/module/block/contract"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	mailcontract "go_wp/internal/module/mail/contract"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	ordercontract "go_wp/internal/module/order/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	productcontract "go_wp/internal/module/product/contract"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"

	"github.com/gin-gonic/gin"
)

// routeDeps 汇总各域注册函数共享的模块契约。
//
// 装配期由 SetupDashboardRoutes 一次填充；各域注册函数只取自己用到的字段，
// 避免每个 setupXxxRoutes 都重复展开十来个契约参数。
type routeDeps struct {
	pages         pagecontract.PageService
	projects      projectcontract.ProjectService
	products      productcontract.ProductService
	templates     contenttemplatecontract.ContentTemplateService
	presentations ProductPagePorts
	contents      contentcontract.ContentService
	inventories   inventorycontract.InventoryService
	masterdata    masterdatacontract.MasterDataService
	mail          mailcontract.MailService
	orders        ordercontract.OrderService
	analytics     analyticscontract.AnalyticsService
	navigations   navigationcontract.NavigationService
	customerAdmin usercontract.CustomerAdminPort
	authz         admincontract.AuthzContextService
}

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
	// contents CMS 内容契约（INF-1 文章管理页）：内容实体此前只有 JSON API，
	// 后台缺入口。本页直接调契约做列表 / 编辑 / 删除（本地调用，不绕回自己的 HTTP API）。
	contents contentcontract.ContentService,
	inventories inventorycontract.InventoryService,
	masterdata masterdatacontract.MasterDataService,
	mail mailcontract.MailService,
	orders ordercontract.OrderService,
	// analytics 访问统计契约（BIZ-8）：只读聚合，页面据此渲染按天 / 按路径报表。
	analytics analyticscontract.AnalyticsService,
	// customerAdmin 用户模块的**后台面**（收窄到四条方法，见 usercontract.CustomerAdminPort）：
	// 客户管理页此前完全不存在 —— users 表有全套字段，但后台没有任何地方读它。
	// 与访客面（/user/*）共用同一个实现，两个面各拿各的接口。
	customerAdmin usercontract.CustomerAdminPort) *Handle {
	if router == nil {
		return nil
	}

	handle := NewHandle(pages, projects, blocks, plugins, collection, admins, roles, perms, menus, depts, rules, authz, navigations)
	handle.SetProductDataSource(products)
	handle.SetTemplateWorkbenchDeps(templates, presentations)

	d := &routeDeps{
		pages:         pages,
		projects:      projects,
		products:      products,
		templates:     templates,
		presentations: presentations,
		contents:      contents,
		inventories:   inventories,
		masterdata:    masterdata,
		mail:          mail,
		orders:        orders,
		analytics:     analytics,
		navigations:   navigations,
		customerAdmin: customerAdmin,
		authz:         authz,
	}

	setupShellRoutes(router, d, handle)

	// /admin/* 后台页面统一挂 Session 认证 + CSRF 校验。
	// 页面内原生 POST 表单已注入 csrf_token 隐藏域（模板），JS fetch 请求统一带 X-CSRF-Token 头。
	adminPages := router.Group("/admin", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), permContextMiddleware(authz))
	setupSiteRoutes(adminPages, d, handle)

	setupCatalogRoutes(adminPages, d, handle)

	setupInventoryRoutes(adminPages, d, handle)

	setupI18nRoutes(adminPages, d, handle)

	setupInsightRoutes(adminPages, d, handle)

	setupOrderRoutes(adminPages, d, handle)

	setupArticleRoutes(adminPages, d, handle)

	setupCustomerRoutes(adminPages, d, handle)

	setupSiteSlotRoutes(adminPages, d, handle)

	setupCouponRoutes(adminPages, d, handle)

	setupProductTranslationWorkbench(adminPages, d)

	setupAdminRoutes(adminPages, d, handle)

	setupMailRoutes(adminPages, d)

	// 返回 handle：上层装配（routes.go）用它注入可选端口（如蓝图契约）。
	return handle
}
