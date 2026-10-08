package adminhttp

// 两个独立入口，装配层按需调用（见 internal/routers/assembly.go）：
//   - SetupAdminPages：/admin 前缀的认证页面组（Session + CSRF + 权限上下文由装配层挂好）；
//   - SetupAdminShellPages：引擎根级注册（登录页，无认证）。
//
// 页面注册与 /api 路由分文件：鉴权扫描按「文件里有没有组级 Casbin」判断整份文件。
// API 路由组挂了 CasbinMiddleware，页面组没有；合在一个文件里，不挂 Casbin 的页面写端点会被扫成已鉴权。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/admin/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
)

// SetupAdminPages 注册 admin 六领域管理页 + 文案词条页 + 语言切换。
//
// adminPages 是装配层创建的 /admin 路由组（Session 认证 + CSRF + 权限上下文）。
// **页面 GET 与写动作都挂 Casbin**，一律复用对应业务 API 的读/写权限点：
// 页面路径在权限点表里不存在，直接以页面路径 enforce 会让所有人（含超管）403，
// 所以必须用 CasbinMiddlewareForPath 指定 API 路径，权限点路径与原 dashboard 版本逐字一致。
//
// 为什么只读页也要鉴权：菜单按权限渲染，但**菜单隐藏不是访问控制** —— 直接输入 URL
// 就能绕过。此前 /admin/administrators 等只读页没有权限门，任何登录的后台账号
// 都能拿到全部管理员的用户名 / 姓名 / 邮箱 / 手机号（handler 直接渲染 AdminList 结果）。
// 权限点全部复用 050_admin_domains_permissions.sql 已有的 GET 权限点，不新增。
// 六个契约由装配层以同一合并 Service 传入（admin 合并模块同包直调）。
// 只用 GET/POST，无 RESTful 路径参数。
func SetupAdminPages(adminPages *gin.RouterGroup,
	admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService,
	pages pagecontract.PageService, dict sysconfigcontract.DictReader) {
	if adminPages == nil {
		return
	}
	handle := NewAdminPagesHandle(admins, roles, perms, menus, depts, rules)

	adminPages.GET("/administrators", builtin.CasbinMiddlewareForPath("/api/admin/list"), handle.AdministratorsPage)
	adminPages.POST("/administrators/create", builtin.CasbinMiddlewareForPath("/api/admin/create"), handle.AdministratorsCreate)
	adminPages.POST("/administrators/update", builtin.CasbinMiddlewareForPath("/api/admin/edit"), handle.AdministratorsUpdate)
	adminPages.POST("/administrators/delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsDelete)
	// 批量删除：逐条走上面的单条删除路径，单条失败不整批回滚（结果由提示页给出，见 admin_jump.go）。
	// 权限点复用单条删除的业务 API，不新增权限点。
	adminPages.POST("/administrators/bulk-delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsBulkDelete)

	// 管理员的授权分配：角色（g 策略）+ 直接额外菜单（p 策略，sub=user_id）。
	// 两件事都挂在管理员列表的行上 ——「这个人能做什么」是一个问题，拆到两个页面会让
	// 「给他加个角色、再补一个菜单入口」变成两次往返。读侧与写侧同权对待：抽屉里就是
	// 该账号的完整授权集合，不挂鉴权等于把它露给任何登录账号（菜单隐藏不是访问控制）。
	// 权限点复用 050 就 seed 的 admin:role_list/save、admin:menu_list/save，不新增。
	adminPages.GET("/administrators/roles/drawer", builtin.CasbinMiddlewareForPath("/api/admin/role/list"), handle.AdministratorRolesDrawer)
	adminPages.POST("/administrators/roles/save", builtin.CasbinMiddlewareForPath("/api/admin/role/save"), handle.AdministratorRolesSave)
	adminPages.GET("/administrators/menus/drawer", builtin.CasbinMiddlewareForPath("/api/admin/menu/list"), handle.AdministratorMenusDrawer)
	adminPages.POST("/administrators/menus/save", builtin.CasbinMiddlewareForPath("/api/admin/menu/save"), handle.AdministratorMenusSave)

	adminPages.GET("/roles", builtin.CasbinMiddlewareForPath("/api/role/list"), handle.RolesPage)
	adminPages.POST("/roles/create", builtin.CasbinMiddlewareForPath("/api/role/create"), handle.RolesCreate)
	adminPages.POST("/roles/update", builtin.CasbinMiddlewareForPath("/api/role/update"), handle.RolesUpdate)
	adminPages.POST("/roles/delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesDelete)
	adminPages.POST("/roles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesBulkDelete)
	// 角色权限分配（角色分权）：抽屉片段 + 保存。读复用 role:menu_list、保存复用 role:menu_save ——
	// 两个权限点在 050 的 seed 里本来就有（它们的 API 路由 /api/role/menu/list 与
	// /api/role/menu/save 同期建成），只是此前没有任何前端调用方；不新增权限点。
	// 片段路由与写路由同权对待：片段里就是该角色的完整勾选集合，不挂鉴权等于把
	// 角色权限表露给任何登录账号（菜单隐藏不是访问控制）。
	adminPages.GET("/roles/permissions/drawer", builtin.CasbinMiddlewareForPath("/api/role/menu/list"), handle.RolePermissionsDrawer)
	adminPages.POST("/roles/permissions/save", builtin.CasbinMiddlewareForPath("/api/role/menu/save"), handle.RolePermissionsSave)

	adminPages.GET("/menus", builtin.CasbinMiddlewareForPath("/api/menu/tree"), handle.MenusPage)
	adminPages.GET("/menus/edit", builtin.CasbinMiddlewareForPathAs("/api/menu/update", "POST"), handle.MenusEditFragment)
	adminPages.POST("/menus/create", builtin.CasbinMiddlewareForPath("/api/menu/create"), handle.MenusCreate)
	adminPages.POST("/menus/update", builtin.CasbinMiddlewareForPath("/api/menu/update"), handle.MenusUpdate)
	adminPages.POST("/menus/delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusDelete)
	adminPages.POST("/menus/bulk-delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusBulkDelete)

	adminPages.GET("/permissions", builtin.CasbinMiddlewareForPath("/api/permission/list"), handle.PermissionsPage)
	adminPages.GET("/permissions/edit", builtin.CasbinMiddlewareForPathAs("/api/permission/update", "POST"), handle.PermissionsEditFragment)
	adminPages.POST("/permissions/create", builtin.CasbinMiddlewareForPath("/api/permission/create"), handle.PermissionsCreate)
	adminPages.POST("/permissions/update", builtin.CasbinMiddlewareForPath("/api/permission/update"), handle.PermissionsUpdate)
	adminPages.POST("/permissions/delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsDelete)
	adminPages.POST("/permissions/bulk-delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsBulkDelete)

	adminPages.GET("/departments", builtin.CasbinMiddlewareForPath("/api/dept/tree"), handle.DepartmentsPage)
	adminPages.POST("/departments/create", builtin.CasbinMiddlewareForPath("/api/dept/create"), handle.DepartmentsCreate)
	adminPages.POST("/departments/update", builtin.CasbinMiddlewareForPath("/api/dept/update"), handle.DepartmentsUpdate)
	adminPages.POST("/departments/delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsDelete)
	adminPages.POST("/departments/bulk-delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsBulkDelete)

	adminPages.GET("/datarules", builtin.CasbinMiddlewareForPath("/api/datarule/list"), handle.DatarulesPage)
	adminPages.GET("/datarules/edit", builtin.CasbinMiddlewareForPath("/api/datarule/detail"), handle.DatarulesEditPage)
	adminPages.POST("/datarules/create", builtin.CasbinMiddlewareForPath("/api/datarule/create"), handle.DatarulesCreate)
	adminPages.POST("/datarules/update", builtin.CasbinMiddlewareForPath("/api/datarule/update"), handle.DatarulesUpdate)
	adminPages.POST("/datarules/delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesDelete)
	adminPages.POST("/datarules/bulk-delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesBulkDelete)
	// 配置编辑器片段：纯渲染、不落库，因此不挂 Casbin（写入仍走 /datarules/update）。
	adminPages.POST("/datarules/config-editor", handle.DataruleConfigEditor)

	// 文案词条页（审计 I18N-003）：读页面挂 i18n:view（迁移 294 新增的只读权限点），
	// 写操作挂 i18n:manage（/api/i18n/save）—— 漏挂等于任何登录管理员都能改全站文案。
	//
	// 为什么读页面要一个**独立的**权限点、不能复用 i18n:manage：CasbinMiddlewareForPath
	// 的 act 取自实际请求方法，页面是 GET 而 i18n:manage 的策略只有 POST ——
	// 复用会让 enforce 匹配不到任何策略，**含超管在内全员 403**。
	i18nPages := NewAdminI18nEntryHandle()
	// 语言下拉的字典只读口（dict 可选：未注入时下拉只有「全部语言」）。
	if dict != nil {
		i18nPages.SetDictReader(dict)
	}
	// 词条变更 → 站点待重建（与页面 / 商品 / 导航翻译、站点设置同一动作）。
	// 漏接的表现是"改了词条站点不更新"，且没有任何报错，故装配期必须接上。
	i18nPages.SetPageMarker(pages)
	// 两个 Casbin 对象都没有对应的 API 路由（词条页只有页面路由），必须在**注册处显式声明**：
	// 否则 permission.RoutesOf 查不到这两条权限点，AI 工具按 fail closed 一律 forbidden，
	// 而页面本身完全正常（AGENTS.md §AI 工具记过这一类）。样板：mail 的 declareMailPageObjects。
	permission.Declare(http.MethodGet, "/api/i18n/list", permission.I18nView)
	permission.Declare(http.MethodPost, "/api/i18n/save", permission.I18nManage)
	adminPages.GET("/i18n", builtin.CasbinMiddlewareForPath("/api/i18n/list"), i18nPages.I18nEntriesPage)
	adminPages.GET("/i18n/edit", builtin.CasbinMiddlewareForPathAs("/api/i18n/save", "POST"), i18nPages.I18nEntryEditFragment)
	adminPages.POST("/i18n/update", builtin.CasbinMiddlewareForPath("/api/i18n/save"), i18nPages.I18nEntryUpdate)
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
