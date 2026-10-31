package adminhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	pagecontract "go_wp/internal/module/page/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"
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

	AdminRoleDrawerTemplate         = "admin/system/admin_role_drawer.html"
	AdminMenuDrawerTemplate         = "admin/system/admin_menu_drawer.html"
	MenuEditFormTemplate            = "admin/system/menu_edit_form.html"
	PermissionEditFormTemplate      = "admin/system/permission_edit_form.html"
	RolePermissionsFragmentTemplate = "admin/system/role_permissions.html"
	I18nEditFormTemplate            = "admin/system/i18n_edit_form.html"
	DataruleConfigEditorTemplate    = "admin/system/datarule_config_editor.html"
)

func SetupAdminPages(adminPages *gin.RouterGroup,
	admins admincontract.AdminService, roles admincontract.RoleService,
	perms admincontract.PermService, menus admincontract.MenuService,
	depts admincontract.DeptService, rules admincontract.RuleService,
	pages pagecontract.PageService, dict sysconfigcontract.DictReader) {
	if adminPages == nil {
		return
	}
	handle := NewAdminPagesHandle(admins, roles, perms, menus, depts, rules)

	adminPages.GET("/administrators",
		adminui.PageAuthz("/api/admin/list"),
		adminui.PageSlot(AdministratorsPageTemplate),
		handle.AdministratorsPage)
	adminPages.POST("/administrators/create", builtin.CasbinMiddlewareForPath("/api/admin/create"), handle.AdministratorsCreate)
	adminPages.POST("/administrators/update", builtin.CasbinMiddlewareForPath("/api/admin/edit"), handle.AdministratorsUpdate)
	adminPages.POST("/administrators/delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsDelete)

	adminPages.POST("/administrators/bulk-delete", builtin.CasbinMiddlewareForPath("/api/admin/delete"), handle.AdministratorsBulkDelete)

	adminPages.GET("/administrators/roles/drawer", builtin.CasbinMiddlewareForPath("/api/admin/role/list"),
		adminui.BareSlot(AdminRoleDrawerTemplate), handle.AdministratorRolesDrawer)
	adminPages.POST("/administrators/roles/save", builtin.CasbinMiddlewareForPath("/api/admin/role/save"), handle.AdministratorRolesSave)
	adminPages.GET("/administrators/menus/drawer", builtin.CasbinMiddlewareForPath("/api/admin/menu/list"),
		adminui.BareSlot(AdminMenuDrawerTemplate), handle.AdministratorMenusDrawer)
	adminPages.POST("/administrators/menus/save", builtin.CasbinMiddlewareForPath("/api/admin/menu/save"), handle.AdministratorMenusSave)

	adminPages.GET("/roles",
		adminui.PageAuthz("/api/role/list"),
		adminui.PageSlot(RolesPageTemplate),
		handle.RolesPage)
	adminPages.POST("/roles/create", builtin.CasbinMiddlewareForPath("/api/role/create"), handle.RolesCreate)
	adminPages.POST("/roles/update", builtin.CasbinMiddlewareForPath("/api/role/update"), handle.RolesUpdate)
	adminPages.POST("/roles/delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesDelete)
	adminPages.POST("/roles/bulk-delete", builtin.CasbinMiddlewareForPath("/api/role/delete"), handle.RolesBulkDelete)

	adminPages.GET("/roles/permissions/drawer", builtin.CasbinMiddlewareForPath("/api/role/menu/list"),
		adminui.BareSlot(RolePermissionsFragmentTemplate), handle.RolePermissionsDrawer)

	adminPages.GET("/menus",
		adminui.PageAuthz("/api/menu/tree"),
		adminui.PageSlot(MenusPageTemplate),
		handle.MenusPage)
	adminPages.GET("/menus/edit", builtin.CasbinMiddlewareForPathAs("/api/menu/update", "POST"),
		adminui.BareSlot(MenuEditFormTemplate), handle.MenusEditFragment)
	adminPages.POST("/menus/create", builtin.CasbinMiddlewareForPath("/api/menu/create"), handle.MenusCreate)
	adminPages.POST("/menus/update", builtin.CasbinMiddlewareForPath("/api/menu/update"), handle.MenusUpdate)
	adminPages.POST("/menus/delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusDelete)
	adminPages.POST("/menus/bulk-delete", builtin.CasbinMiddlewareForPath("/api/menu/delete"), handle.MenusBulkDelete)

	adminPages.GET("/permissions", adminui.PageAuthz("/api/permission/list"),
		adminui.PageSlot(PermissionsPageTemplate), handle.PermissionsPage)
	adminPages.GET("/permissions/edit", builtin.CasbinMiddlewareForPathAs("/api/permission/update", "POST"),
		adminui.BareSlot(PermissionEditFormTemplate), handle.PermissionsEditFragment)
	adminPages.POST("/permissions/create", builtin.CasbinMiddlewareForPath("/api/permission/create"), handle.PermissionsCreate)
	adminPages.POST("/permissions/update", builtin.CasbinMiddlewareForPath("/api/permission/update"), handle.PermissionsUpdate)
	adminPages.POST("/permissions/delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsDelete)
	adminPages.POST("/permissions/bulk-delete", builtin.CasbinMiddlewareForPath("/api/permission/delete"), handle.PermissionsBulkDelete)

	adminPages.GET("/departments",
		adminui.PageAuthz("/api/dept/tree"),
		adminui.PageSlot(DepartmentsPageTemplate),
		handle.DepartmentsPage)
	adminPages.POST("/departments/create", builtin.CasbinMiddlewareForPath("/api/dept/create"), handle.DepartmentsCreate)
	adminPages.POST("/departments/update", builtin.CasbinMiddlewareForPath("/api/dept/update"), handle.DepartmentsUpdate)
	adminPages.POST("/departments/delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsDelete)
	adminPages.POST("/departments/bulk-delete", builtin.CasbinMiddlewareForPath("/api/dept/delete"), handle.DepartmentsBulkDelete)

	adminPages.GET("/datarules",
		adminui.PageAuthz("/api/datarule/list"),
		adminui.PageSlot(DatarulesPageTemplate),
		handle.DatarulesPage)
	adminPages.GET("/datarules/edit", adminui.PageAuthz("/api/datarule/detail"),
		adminui.PageSlot(DataruleEditPageTemplate), handle.DatarulesEditPage)
	adminPages.POST("/datarules/create", builtin.CasbinMiddlewareForPath("/api/datarule/create"), handle.DatarulesCreate)
	adminPages.POST("/datarules/update", builtin.CasbinMiddlewareForPath("/api/datarule/update"), handle.DatarulesUpdate)
	adminPages.POST("/datarules/delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesDelete)
	adminPages.POST("/datarules/bulk-delete", builtin.CasbinMiddlewareForPath("/api/datarule/delete"), handle.DatarulesBulkDelete)

	adminPages.POST("/datarules/config-editor",
		adminui.BareSlot(DataruleConfigEditorTemplate), handle.DataruleConfigEditor)

	i18nPages := adminI18nHandle()

	if dict != nil {
		i18nPages.SetDictReader(dict)
	}

	i18nPages.SetPageMarker(pages)

	permission.Declare(http.MethodGet, "/api/i18n/list", permission.I18nView)
	adminPages.GET("/i18n", adminui.PageAuthz("/api/i18n/list"),
		adminui.PageSlot(I18nPageTemplate), i18nPages.I18nEntriesPage)
	adminPages.GET("/i18n/edit", builtin.CasbinMiddlewareForPathAs("/api/i18n/save", "POST"),
		adminui.BareSlot(I18nEditFormTemplate), i18nPages.I18nEntryEditFragment)

	adminPages.GET("/lang", AdminLangSwitch)
}

func SetupAdminShellPages(router *gin.Engine) {
	if router == nil {
		return
	}
	router.GET("/admin/login", adminui.PageSlot(LoginPageTemplate), AdminLoginPage)
}
