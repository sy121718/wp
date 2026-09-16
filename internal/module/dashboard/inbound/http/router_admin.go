package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_admin.go - admin 六领域管理页路由（管理员/角色/菜单/权限/部门/数据权限）。

func setupAdminRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
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
