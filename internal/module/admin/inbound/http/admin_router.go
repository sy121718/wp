package adminhttp

import (
	"time"

	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/admin/contract"
	"go_wp/internal/module/admin/service"
	"go_wp/internal/permission"
)

// 登录接口按 IP 限流参数：与验证码限流 + 失败计数原子锁定构成三层防线，
// 防止匿名脚本并发爆破登录。正常用户 60s 内登录尝试不会超过 10 次。
const (
	loginRateLimit  = 10
	loginRateWindow = time.Minute
)

// SetupAdminRoutes 装配 admin 模块（管理员/角色/权限点/菜单/部门/数据权限）并注册全部路由。
//
// 合并后模块内部同包直调，无跨模块契约，不对外暴露接口。
// 返回 AuthzContextService：对外只读权限上下文查询能力，供外部模块/插件消费
// （管理面写操作仍由 handle 层经 AdminService 等走 Casbin 鉴权，不在此返回）。
func SetupAdminRoutes(rg *permission.RouteGroup, db *gorm.DB) admincontract.AuthzContextService {
	if rg == nil {
		return nil
	}

	svc := adminservice.NewService(db)
	handle := NewHandle(svc)

	// 数据权限装配（域注册 + RuleProvider + GORM 插件）在路由注册之前完成：
	// 域没注册上的表不会被任何规则拦住，晚一步就是一段静默不设防的窗口。
	bootstrapDataRule(svc, db)

	// --- 管理员 ---
	admin := rg.Group("/admin")
	// 登录接口匿名可达，无条件挂按 IP 限流（不依赖全局 rate_limit 开关）。
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

	// --- 角色 ---
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

	// --- 权限点 ---
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

	// --- 菜单 ---
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

	// --- 部门 ---
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

	// --- 数据权限规则 ---
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

	return svc
}
