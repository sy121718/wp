package adminhttp

import (
	"time"

	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminenums "go_wp/internal/module/admin/enums"
	adminservice "go_wp/internal/module/admin/service"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/view"
)

const (
	loginRateLimit  = 10
	loginRateWindow = time.Minute
)

func SetupAdminRoutes(rg *mw.RouteGroup, db *gorm.DB) admincontract.AuthzContextService {
	if rg == nil {
		return nil
	}

	svc := adminservice.NewService(db)
	handle := NewHandle(svc)

	bootstrapDataRule(svc, db)

	admin := rg.Group("/admin")

	admin.ExemptPOST("/login",
		builtin.RequestRateLimitMiddleware(loginRateLimit, loginRateWindow),
		handle.AdminLogin)

	auth := admin.Group("").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.DataRuleContextMiddleware(),
	)
	{
		auth.ExemptPOST("/logout", handle.AdminLogout)
		auth.ExemptGET("/profile", handle.AdminProfile)
	}

	authorized := admin.Group("").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
		builtin.DataRuleContextMiddleware(),
	)
	{
		authorized.GET("/list", adminenums.AdminList, handle.AdminList)
		authorized.GET("/detail", adminenums.AdminDetail, handle.AdminDetail)
		authorized.POST("/create", adminenums.AdminCreate, handle.AdminCreate)
		authorized.POST("/edit", adminenums.AdminEdit, handle.AdminEdit)
		authorized.POST("/delete", adminenums.AdminDelete, handle.AdminDelete)
		authorized.GET("/role/list", adminenums.AdminRoleList, handle.AdminRoleList)
		authorized.POST("/role/save", adminenums.AdminRoleSave, view.HTMLFragment, handle.AdminRoleSave)
		authorized.GET("/menu/list", adminenums.AdminMenuList, handle.AdminMenuList)
		authorized.POST("/menu/save", adminenums.AdminMenuSave, view.HTMLFragment, handle.AdminMenuSave)
	}

	role := rg.Group("/role").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		role.GET("/list", adminenums.RoleList, handle.RoleList)
		role.GET("/detail", adminenums.RoleDetail, handle.RoleDetail)
		role.POST("/create", adminenums.RoleCreate, handle.RoleCreate)
		role.POST("/update", adminenums.RoleUpdate, handle.RoleUpdate)
		role.POST("/delete", adminenums.RoleDelete, handle.RoleDelete)
		role.GET("/menu/list", adminenums.RoleMenuList, handle.RoleMenuList)
		role.POST("/menu/save", adminenums.RoleMenuSave, view.HTMLFragment, handle.RoleMenuSave)
		role.GET("/user/list", adminenums.RoleUserList, handle.RoleUserList)
		role.POST("/user/save", adminenums.RoleUserSave, handle.RoleUserSave)
	}

	perm := rg.Group("/permission").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		perm.GET("/list", adminenums.PermissionList, handle.PermList)
		perm.GET("/detail", adminenums.PermissionDetail, handle.PermDetail)
		perm.GET("/options", adminenums.PermissionOptions, handle.PermOptions)
		perm.POST("/create", adminenums.PermissionCreate, handle.PermCreate)
		perm.POST("/update", adminenums.PermissionUpdate, handle.PermUpdate)
		perm.POST("/delete", adminenums.PermissionDelete, handle.PermDelete)
	}

	menu := rg.Group("/menu").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		menu.GET("/tree", adminenums.MenuList, handle.MenuTree)
		menu.GET("/detail", adminenums.MenuDetail, handle.MenuDetail)
		menu.POST("/create", adminenums.MenuCreate, handle.MenuCreate)
		menu.POST("/update", adminenums.MenuUpdate, handle.MenuUpdate)
		menu.POST("/delete", adminenums.MenuDelete, handle.MenuDelete)
	}

	dept := rg.Group("/dept").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		dept.GET("/tree", adminenums.DeptList, handle.DeptTree)
		dept.GET("/detail", adminenums.DeptDetail, handle.DeptDetail)
		dept.POST("/create", adminenums.DeptCreate, handle.DeptCreate)
		dept.POST("/update", adminenums.DeptUpdate, handle.DeptUpdate)
		dept.POST("/delete", adminenums.DeptDelete, handle.DeptDelete)
		dept.GET("/user/list", adminenums.DeptUserList, handle.DeptUserList)
		dept.POST("/user/save", adminenums.DeptUserSave, handle.DeptUserSave)
	}

	datarule := rg.Group("/datarule").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		datarule.GET("/list", adminenums.DataruleList, handle.RuleList)
		datarule.GET("/detail", adminenums.DataruleDetail, handle.RuleDetail)
		datarule.POST("/create", adminenums.DataruleCreate, handle.RuleCreate)
		datarule.POST("/update", adminenums.DataruleUpdate, handle.RuleUpdate)
		datarule.POST("/delete", adminenums.DataruleDelete, handle.RuleDelete)
		datarule.GET("/schema/list", adminenums.DataruleSchemaList, handle.RuleSchemaList)
		datarule.GET("/schema/detail", adminenums.DataruleSchemaDetail, handle.RuleSchemaDetail)
		datarule.GET("/assignment/list", adminenums.DataruleAssignmentList, handle.RuleAssignmentList)
		datarule.POST("/assignment/save", adminenums.DataruleAssignmentSave, handle.RuleAssignmentSave)
	}

	i18nAPI := rg.Group("/i18n").Use(
		builtin.SessionAuthMiddleware(),
		builtin.CSRFMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		i18nAPI.POST("/save", adminenums.I18nManage, view.HTMLFragment, adminI18nHandle().I18nEntryWrite)
	}

	return svc
}
