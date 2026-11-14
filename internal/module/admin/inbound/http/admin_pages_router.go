package adminhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminenums "go_wp/internal/module/admin/enums"
	pagecontract "go_wp/internal/module/page/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/view"
)

const (
	AdministratorsPageTemplate = "admin/system/administrators"
	DepartmentsPageTemplate    = "admin/system/departments"
	DatarulesPageTemplate      = "admin/system/datarules"
	DataruleEditPageTemplate   = "admin/system/datarule_edit"
	MenusPageTemplate          = "admin/system/menus"
	RolesPageTemplate          = "admin/system/roles"
	PermissionsPageTemplate    = "admin/system/permissions"
	I18nPageTemplate           = "admin/system/i18n"
	LoginPageTemplate          = "admin/login"
)

func SetupAdminPages(reg *mw.Registry, adminPages *gin.RouterGroup,
	admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService,
	pages pagecontract.PageService, dict sysconfigcontract.DictReader) {
	if adminPages == nil {
		return
	}
	handle := NewAdminPagesHandle(admins, roles, perms, menus, depts, rules)

	adminPages.GET("/administrators",
		mw.PageAuthz("/api/admin/list"),
		view.Slot(AdministratorsPageTemplate, view.RenderPage),
		handle.AdministratorsPage)
	mw.DeclarePageGetOn(reg, "/admin/administrators", "/api/admin/list")
	adminPages.POST("/administrators/create", builtin.CasbinMiddlewareForPath("/api/admin/create"), handle.AdministratorsCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/administrators/create", "/api/admin/create", http.MethodPost)
	}

	adminPages.POST("/administrators/update", builtin.CasbinMiddlewareForPath("/api/admin/edit"), handle.AdministratorsUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/administrators/update", "/api/admin/edit", http.MethodPost)
	}

	adminPages.POST("/administrators/delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/administrators/delete", "/api/admin/delete", http.MethodPost)
	}

	adminPages.POST("/administrators/bulk-delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/administrators/bulk-delete", "/api/admin/delete", http.MethodPost)
	}

	adminPages.GET("/administrators/roles/drawer", view.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/admin/role/list"),
		handle.AdministratorRolesDrawer)
	mw.DeclarePageGetOn(reg, "/admin/administrators/roles/drawer", "/api/admin/role/list")
	adminPages.GET("/administrators/menus/drawer", view.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/admin/menu/list"),
		handle.AdministratorMenusDrawer)
	mw.DeclarePageGetOn(reg, "/admin/administrators/menus/drawer", "/api/admin/menu/list")

	adminPages.GET("/roles",
		mw.PageAuthz("/api/role/list"),
		view.Slot(RolesPageTemplate, view.RenderPage),
		handle.RolesPage)
	mw.DeclarePageGetOn(reg, "/admin/roles", "/api/role/list")
	adminPages.POST("/roles/create", builtin.CasbinMiddlewareForPath("/api/role/create"), handle.RolesCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/roles/create", "/api/role/create", http.MethodPost)
	}

	adminPages.POST("/roles/update", builtin.CasbinMiddlewareForPath("/api/role/update"), handle.RolesUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/roles/update", "/api/role/update", http.MethodPost)
	}

	adminPages.POST("/roles/delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/roles/delete", "/api/role/delete", http.MethodPost)
	}

	adminPages.POST("/roles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/roles/bulk-delete", "/api/role/delete", http.MethodPost)
	}

	adminPages.GET("/roles/permissions/drawer", view.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/role/menu/list"),
		handle.RolePermissionsDrawer)
	mw.DeclarePageGetOn(reg, "/admin/roles/permissions/drawer", "/api/role/menu/list")

	adminPages.GET("/menus",
		mw.PageAuthz("/api/menu/tree"),
		view.Slot(MenusPageTemplate, view.RenderPage),
		handle.MenusPage)
	mw.DeclarePageGetOn(reg, "/admin/menus", "/api/menu/tree")
	adminPages.GET("/menus/edit", view.HTMLFragment, builtin.CasbinMiddlewareForPathAs("/api/menu/update", "POST"),
		handle.MenusEditFragment)
	mw.DeclarePageGetAsOn(reg, "/admin/menus/edit", "/api/menu/update", http.MethodPost)
	adminPages.POST("/menus/create", builtin.CasbinMiddlewareForPath("/api/menu/create"), handle.MenusCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/menus/create", "/api/menu/create", http.MethodPost)
	}

	adminPages.POST("/menus/update", view.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/menu/update"), handle.MenusUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/menus/update", "/api/menu/update", http.MethodPost)
	}

	adminPages.POST("/menus/delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/menus/delete", "/api/menu/delete", http.MethodPost)
	}

	adminPages.POST("/menus/bulk-delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/menus/bulk-delete", "/api/menu/delete", http.MethodPost)
	}

	adminPages.GET("/permissions", mw.PageAuthz("/api/permission/list"),
		view.Slot(PermissionsPageTemplate, view.RenderPage), handle.PermissionsPage)
	mw.DeclarePageGetOn(reg, "/admin/permissions", "/api/permission/list")
	adminPages.GET("/permissions/edit", view.HTMLFragment, builtin.CasbinMiddlewareForPathAs("/api/permission/update", "POST"),
		handle.PermissionsEditFragment)
	mw.DeclarePageGetAsOn(reg, "/admin/permissions/edit", "/api/permission/update", http.MethodPost)
	adminPages.POST("/permissions/create", builtin.CasbinMiddlewareForPath("/api/permission/create"), handle.PermissionsCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/permissions/create", "/api/permission/create", http.MethodPost)
	}

	adminPages.POST("/permissions/update", view.HTMLFragment, builtin.CasbinMiddlewareForPath("/api/permission/update"), handle.PermissionsUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/permissions/update", "/api/permission/update", http.MethodPost)
	}

	adminPages.POST("/permissions/delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/permissions/delete", "/api/permission/delete", http.MethodPost)
	}

	adminPages.POST("/permissions/bulk-delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/permissions/bulk-delete", "/api/permission/delete", http.MethodPost)
	}

	adminPages.GET("/departments",
		mw.PageAuthz("/api/dept/tree"),
		view.Slot(DepartmentsPageTemplate, view.RenderPage),
		handle.DepartmentsPage)
	mw.DeclarePageGetOn(reg, "/admin/departments", "/api/dept/tree")
	adminPages.POST("/departments/create", builtin.CasbinMiddlewareForPath("/api/dept/create"), handle.DepartmentsCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/departments/create", "/api/dept/create", http.MethodPost)
	}

	adminPages.POST("/departments/update", builtin.CasbinMiddlewareForPath("/api/dept/update"), handle.DepartmentsUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/departments/update", "/api/dept/update", http.MethodPost)
	}

	adminPages.POST("/departments/delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/departments/delete", "/api/dept/delete", http.MethodPost)
	}

	adminPages.POST("/departments/bulk-delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/departments/bulk-delete", "/api/dept/delete", http.MethodPost)
	}

	adminPages.GET("/datarules",
		mw.PageAuthz("/api/datarule/list"),
		view.Slot(DatarulesPageTemplate, view.RenderPage),
		handle.DatarulesPage)
	mw.DeclarePageGetOn(reg, "/admin/datarules", "/api/datarule/list")
	adminPages.GET("/datarules/edit", mw.PageAuthz("/api/datarule/detail"),
		view.Slot(DataruleEditPageTemplate, view.RenderPage), handle.DatarulesEditPage)
	mw.DeclarePageGetOn(reg, "/admin/datarules/edit", "/api/datarule/detail")
	adminPages.POST("/datarules/create", builtin.CasbinMiddlewareForPath("/api/datarule/create"), handle.DatarulesCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/datarules/create", "/api/datarule/create", http.MethodPost)
	}

	adminPages.POST("/datarules/update", builtin.CasbinMiddlewareForPath("/api/datarule/update"), handle.DatarulesUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/datarules/update", "/api/datarule/update", http.MethodPost)
	}

	adminPages.POST("/datarules/delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/datarules/delete", "/api/datarule/delete", http.MethodPost)
	}

	adminPages.POST("/datarules/bulk-delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesBulkDelete)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/datarules/bulk-delete", "/api/datarule/delete", http.MethodPost)
	}

	i18nPages := adminI18nHandle()

	if dict != nil {
		i18nPages.SetDictReader(dict)
	}

	i18nPages.SetPageMarker(pages)

	reg.DeclareRoute(mw.RouteSpec{
		Method: http.MethodGet, Path: "/api/i18n/list",
		Perm: adminenums.I18nView, Kind: mw.RoutePage,
		AuthObject: "/api/i18n/list", AuthMethod: http.MethodGet,
	})
	adminPages.GET("/i18n", view.HTMLPage, mw.PageAuthz("/api/i18n/list"),
		view.Slot(I18nPageTemplate, view.RenderPage), i18nPages.I18nEntriesPage)
	mw.DeclarePageGetOn(reg, "/admin/i18n", "/api/i18n/list")
	adminPages.GET("/i18n/edit", view.HTMLFragment, builtin.CasbinMiddlewareForPathAs("/api/i18n/save", "POST"),
		i18nPages.I18nEntryEditFragment)
	mw.DeclarePageGetAsOn(reg, "/admin/i18n/edit", "/api/i18n/save", http.MethodPost)

	// 语言切换只改语言 cookie 并回到原页：它不读业务数据，但仍是后台入口，
	// 鉴权对象取后台管理员的列表操作（浏览器只需已登录后台）。
	adminPages.GET("/lang", mw.PageAuthz("/api/admin/list"), AdminLangSwitch)
	mw.DeclarePageGetOn(reg, "/admin/lang", "/api/admin/list")
}

func SetupAdminShellPages(router *gin.Engine) {
	if router == nil {
		return
	}
	router.GET("/admin/login", view.Slot(LoginPageTemplate, view.RenderPage), AdminLoginPage)
}
