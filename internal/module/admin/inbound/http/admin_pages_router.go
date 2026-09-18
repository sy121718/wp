package adminhttp

// admin_pages_router.go — admin 模块页面路由入口（自 dashboard 模块搬回）。
//
// 两个独立入口，装配层按需调用（见 internal/routers/assembly.go）：
//   - SetupAdminPages：/admin 前缀的认证页面组（Session + CSRF + 权限上下文由装配层挂好）；
//   - SetupAdminShellPages：引擎根级注册（登录页，无认证）。

import (
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"

	"github.com/gin-gonic/gin"
)

// SetupAdminPages 注册 admin 六领域管理页 + 文案词条页 + 语言切换。
//
// adminPages 是装配层创建的 /admin 路由组（Session 认证 + CSRF + 权限上下文）。
// 页面 GET 走组认证（无 Casbin）；写动作（create/update/delete）挂对应业务 API
// 权限点做 Casbin 鉴权，权限点路径与原 dashboard 版本逐字一致。
// 六个契约由装配层以同一合并 Service 传入（admin 合并模块同包直调）。
// 只用 GET/POST，无 RESTful 路径参数。
func SetupAdminPages(adminPages *gin.RouterGroup,
	admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService) {
	if adminPages == nil {
		return
	}
	handle := NewAdminPagesHandle(admins, roles, perms, menus, depts, rules)

	adminPages.GET("/administrators", handle.AdministratorsPage)
	adminPages.POST("/administrators/create", builtin.CasbinMiddlewareForPath("/api/admin/create"), handle.AdministratorsCreate)
	adminPages.POST("/administrators/update", builtin.CasbinMiddlewareForPath("/api/admin/edit"), handle.AdministratorsUpdate)
	adminPages.POST("/administrators/delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsDelete)
	// 批量删除：逐条走上面的单条删除路径，单条失败不整批回滚（结果经 ?done=/?err= 回带）。
	// 权限点复用单条删除的业务 API，不新增权限点。
	adminPages.POST("/administrators/bulk-delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsBulkDelete)

	adminPages.GET("/roles", handle.RolesPage)
	adminPages.POST("/roles/create", builtin.CasbinMiddlewareForPath("/api/role/create"), handle.RolesCreate)
	adminPages.POST("/roles/update", builtin.CasbinMiddlewareForPath("/api/role/update"), handle.RolesUpdate)
	adminPages.POST("/roles/delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesDelete)
	adminPages.POST("/roles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesBulkDelete)

	adminPages.GET("/menus", handle.MenusPage)
	adminPages.POST("/menus/create", builtin.CasbinMiddlewareForPath("/api/menu/create"), handle.MenusCreate)
	adminPages.POST("/menus/update", builtin.CasbinMiddlewareForPath("/api/menu/update"), handle.MenusUpdate)
	adminPages.POST("/menus/delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusDelete)
	adminPages.POST("/menus/bulk-delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusBulkDelete)

	adminPages.GET("/permissions", handle.PermissionsPage)
	adminPages.POST("/permissions/create", builtin.CasbinMiddlewareForPath("/api/permission/create"), handle.PermissionsCreate)
	adminPages.POST("/permissions/update", builtin.CasbinMiddlewareForPath("/api/permission/update"), handle.PermissionsUpdate)
	adminPages.POST("/permissions/delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsDelete)
	adminPages.POST("/permissions/bulk-delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsBulkDelete)

	adminPages.GET("/departments", handle.DepartmentsPage)
	adminPages.POST("/departments/create", builtin.CasbinMiddlewareForPath("/api/dept/create"), handle.DepartmentsCreate)
	adminPages.POST("/departments/update", builtin.CasbinMiddlewareForPath("/api/dept/update"), handle.DepartmentsUpdate)
	adminPages.POST("/departments/delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsDelete)
	adminPages.POST("/departments/bulk-delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsBulkDelete)

	adminPages.GET("/datarules", handle.DatarulesPage)
	adminPages.GET("/datarules/edit", handle.DatarulesEditPage)
	adminPages.POST("/datarules/create", builtin.CasbinMiddlewareForPath("/api/datarule/create"), handle.DatarulesCreate)
	adminPages.POST("/datarules/update", builtin.CasbinMiddlewareForPath("/api/datarule/update"), handle.DatarulesUpdate)
	adminPages.POST("/datarules/delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesDelete)
	adminPages.POST("/datarules/bulk-delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesBulkDelete)
	// 配置编辑器片段：纯渲染、不落库，因此不挂 Casbin（写入仍走 /datarules/update）。
	adminPages.POST("/datarules/config-editor", handle.DataruleConfigEditor)

	// 文案词条页（审计 I18N-003）：读页面不挂 Casbin（与其它只读页一致），
	// 写操作挂 i18n:manage（/api/i18n/save）——漏挂等于任何登录管理员都能改全站文案。
	i18nPages := NewAdminI18nEntryHandle()
	adminPages.GET("/i18n", i18nPages.I18nEntriesPage)
	adminPages.POST("/i18n/save", builtin.CasbinMiddlewareForPath("/api/i18n/save"), i18nPages.I18nEntrySave)
	adminPages.POST("/i18n/delete", builtin.CasbinMiddlewareForPath("/api/i18n/save"), i18nPages.I18nEntryDelete)
	// 批量删除复用同一条删除路径与权限点（i18n:manage → /api/i18n/save）：
	// 批量只是单条的加速器，不是另一件事；另立权限点会长出
	// 「能删一条、不能删十条」这种没人能解释的状态。
	adminPages.POST("/i18n/bulk-delete", builtin.CasbinMiddlewareForPath("/api/i18n/save"), i18nPages.I18nEntriesBulkDelete)

	// 语言切换（多语言 P1）：GET 属安全方法，写语言 Cookie 后 302 回跳。
	adminPages.GET("/lang", AdminLangSwitch)
}

// SetupAdminShellPages 注册引擎根级外壳页面：登录页（无认证）。
//
// 登录页本身不能挂 Session 中间件 —— 未登录请求被中间件 302 到这里，
// 挂了就成循环跳转。debug 一键登录开关判断随页面一并迁移（AdminDevLoginEnabled）。
func SetupAdminShellPages(router *gin.Engine) {
	if router == nil {
		return
	}
	router.GET("/admin/login", AdminLoginPage)
}
