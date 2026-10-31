package adminhttp

import (
	"time"

	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/internal/permission"
)

const (
	loginRateLimit  = 10
	loginRateWindow = time.Minute
)

func SetupAdminRoutes(rg *permission.RouteGroup, db *gorm.DB) admincontract.AuthzContextService {
	if rg == nil {
		return nil
	}

	svc := adminservice.NewService(db)
	handle := NewHandle(svc)

	bootstrapDataRule(svc, db)

	admin := rg.Group("/admin")

	admin.POST("/login", permission.Exempt,
		builtin.RequestRateLimitMiddleware(loginRateLimit, loginRateWindow),
		handle.AdminLogin)

	auth := admin.Group("").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.DataRuleContextMiddleware(),
	)
	{
		auth.POST("/logout", permission.Exempt, handle.AdminLogout)
		auth.GET("/profile", permission.Exempt, handle.AdminProfile)
	}

	authorized := admin.Group("").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
		builtin.DataRuleContextMiddleware(),
	)
	{
		authorized.GET("/list", permission.AdminList, handle.AdminList)
		authorized.GET("/detail", permission.AdminDetail, handle.AdminDetail)
		authorized.POST("/create", permission.AdminCreate, handle.AdminCreate)
		authorized.POST("/edit", permission.AdminEdit, handle.AdminEdit)
		authorized.POST("/delete", permission.AdminDelete, handle.AdminDelete)
		authorized.GET("/role/list", permission.AdminRoleList, handle.AdminRoleList)
		authorized.POST("/role/save", permission.AdminRoleSave, handle.AdminRoleSave)
		authorized.GET("/menu/list", permission.AdminMenuList, handle.AdminMenuList)
		authorized.POST("/menu/save", permission.AdminMenuSave, handle.AdminMenuSave)
	}

	role := rg.Group("/role").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		role.GET("/list", permission.RoleList, handle.RoleList)
		role.GET("/detail", permission.RoleDetail, handle.RoleDetail)
		role.POST("/create", permission.RoleCreate, handle.RoleCreate)
		role.POST("/update", permission.RoleUpdate, handle.RoleUpdate)
		role.POST("/delete", permission.RoleDelete, handle.RoleDelete)
		role.GET("/menu/list", permission.RoleMenuList, handle.RoleMenuList)
		role.POST("/menu/save", permission.RoleMenuSave, handle.RoleMenuSave)
		role.GET("/user/list", permission.RoleUserList, handle.RoleUserList)
		role.POST("/user/save", permission.RoleUserSave, handle.RoleUserSave)
	}

	perm := rg.Group("/permission").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		perm.GET("/list", permission.PermissionList, handle.PermList)
		perm.GET("/detail", permission.PermissionDetail, handle.PermDetail)
		perm.GET("/options", permission.PermissionOptions, handle.PermOptions)
		perm.POST("/create", permission.PermissionCreate, handle.PermCreate)
		perm.POST("/update", permission.PermissionUpdate, handle.PermUpdate)
		perm.POST("/delete", permission.PermissionDelete, handle.PermDelete)
	}

	menu := rg.Group("/menu").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		menu.GET("/tree", permission.MenuList, handle.MenuTree)
		menu.GET("/detail", permission.MenuDetail, handle.MenuDetail)
		menu.POST("/create", permission.MenuCreate, handle.MenuCreate)
		menu.POST("/update", permission.MenuUpdate, handle.MenuUpdate)
		menu.POST("/delete", permission.MenuDelete, handle.MenuDelete)
	}

	dept := rg.Group("/dept").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		dept.GET("/tree", permission.DeptList, handle.DeptTree)
		dept.GET("/detail", permission.DeptDetail, handle.DeptDetail)
		dept.POST("/create", permission.DeptCreate, handle.DeptCreate)
		dept.POST("/update", permission.DeptUpdate, handle.DeptUpdate)
		dept.POST("/delete", permission.DeptDelete, handle.DeptDelete)
		dept.GET("/user/list", permission.DeptUserList, handle.DeptUserList)
		dept.POST("/user/save", permission.DeptUserSave, handle.DeptUserSave)
	}

	datarule := rg.Group("/datarule").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		datarule.GET("/list", permission.DataruleList, handle.RuleList)
		datarule.GET("/detail", permission.DataruleDetail, handle.RuleDetail)
		datarule.POST("/create", permission.DataruleCreate, handle.RuleCreate)
		datarule.POST("/update", permission.DataruleUpdate, handle.RuleUpdate)
		datarule.POST("/delete", permission.DataruleDelete, handle.RuleDelete)
		datarule.GET("/schema/list", permission.DataruleSchemaList, handle.RuleSchemaList)
		datarule.GET("/schema/detail", permission.DataruleSchemaDetail, handle.RuleSchemaDetail)
		datarule.GET("/assignment/list", permission.DataruleAssignmentList, handle.RuleAssignmentList)
		datarule.POST("/assignment/save", permission.DataruleAssignmentSave, handle.RuleAssignmentSave)
	}

	i18nAPI := rg.Group("/i18n").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		i18nAPI.POST("/save", permission.I18nManage, adminI18nHandle().I18nEntryWrite)
	}

	return svc
}
