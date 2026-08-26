package adminhttp

import (
	"log"

	"go_wp/internal/middleware/builtin"
	adminservice "go_wp/internal/module/admin/service"
	datarulepkg "go_wp/pkg/datarule"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupAdminRoutes 装配 admin 模块（管理员/角色/权限点/菜单/部门/数据权限）并注册全部路由。
//
// 合并后模块内部同包直调，无跨模块契约，不对外暴露接口。
func SetupAdminRoutes(rg *gin.RouterGroup, db *gorm.DB) {
	if rg == nil {
		return
	}

	svc := adminservice.NewService(db)
	handle := NewHandle(svc)

	// 注册数据权限 RuleProvider 到 datarule 引擎
	datarulepkg.SetProvider(svc)
	if err := datarulepkg.RegisterPluginWithDB(db); err != nil {
		log.Printf("注册 datarule GORM 插件失败: %v", err)
	}

	// --- 管理员 ---
	admin := rg.Group("/admin")
	admin.POST("/login", handle.AdminLogin)

	auth := admin.Group("").Use(builtin.JWTAuthMiddleware(), builtin.DataRuleContextMiddleware())
	{
		auth.POST("/logout", handle.AdminLogout)
		auth.GET("/profile", handle.AdminProfile)
		auth.GET("/routes", handle.AdminRoutes)
	}

	authorized := admin.Group("").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
		builtin.DataRuleContextMiddleware(),
	)
	{
		authorized.GET("/list", handle.AdminList)
		authorized.GET("/detail", handle.AdminDetail)
		authorized.POST("/create", handle.AdminCreate)
		authorized.POST("/edit", handle.AdminEdit)
		authorized.POST("/delete", handle.AdminDelete)
		authorized.GET("/role/list", handle.AdminRoleList)
		authorized.POST("/role/save", handle.AdminRoleSave)
		authorized.GET("/menu/list", handle.AdminMenuList)
		authorized.POST("/menu/save", handle.AdminMenuSave)
	}

	// --- 角色 ---
	role := rg.Group("/role").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		role.GET("/list", handle.RoleList)
		role.GET("/detail", handle.RoleDetail)
		role.POST("/create", handle.RoleCreate)
		role.POST("/update", handle.RoleUpdate)
		role.POST("/delete", handle.RoleDelete)
		role.GET("/menu/list", handle.RoleMenuList)
		role.POST("/menu/save", handle.RoleMenuSave)
		role.GET("/user/list", handle.RoleUserList)
		role.POST("/user/save", handle.RoleUserSave)
	}

	// --- 权限点 ---
	perm := rg.Group("/permission").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		perm.GET("/list", handle.PermList)
		perm.GET("/detail", handle.PermDetail)
		perm.GET("/options", handle.PermOptions)
		perm.POST("/create", handle.PermCreate)
		perm.POST("/update", handle.PermUpdate)
		perm.POST("/delete", handle.PermDelete)
	}

	// --- 菜单 ---
	menu := rg.Group("/menu").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		menu.GET("/tree", handle.MenuTree)
		menu.GET("/detail", handle.MenuDetail)
		menu.POST("/create", handle.MenuCreate)
		menu.POST("/update", handle.MenuUpdate)
		menu.POST("/delete", handle.MenuDelete)
	}

	// --- 部门 ---
	dept := rg.Group("/dept").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		dept.GET("/tree", handle.DeptTree)
		dept.GET("/detail", handle.DeptDetail)
		dept.POST("/create", handle.DeptCreate)
		dept.POST("/update", handle.DeptUpdate)
		dept.POST("/delete", handle.DeptDelete)
		dept.GET("/user/list", handle.DeptUserList)
		dept.POST("/user/save", handle.DeptUserSave)
	}

	// --- 数据权限规则 ---
	registerDomains()
	datarule := rg.Group("/datarule").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		datarule.GET("/list", handle.RuleList)
		datarule.GET("/detail", handle.RuleDetail)
		datarule.POST("/create", handle.RuleCreate)
		datarule.POST("/update", handle.RuleUpdate)
		datarule.POST("/delete", handle.RuleDelete)
		datarule.GET("/schema/list", handle.RuleSchemaList)
		datarule.GET("/schema/detail", handle.RuleSchemaDetail)
		datarule.GET("/assignment/list", handle.RuleAssignmentList)
		datarule.POST("/assignment/save", handle.RuleAssignmentSave)
	}
}

// registerDomains 注册所有数据域及字段白名单。
func registerDomains() {
	datarulepkg.RegisterDomain(datarulepkg.DomainConfig{
		Domain:      "ADMIN",
		DomainLabel: "管理员",
		TableName:   "sys_admin",
		WhiteList: []datarulepkg.FieldDef{
			{Field: "username", Label: "用户名", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE", "NOT_LIKE"}},
			{Field: "email", Label: "邮箱", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE"}},
			{Field: "phone", Label: "手机号", DataType: "varchar", Operators: []string{"EQ", "NEQ"}},
			{Field: "status", Label: "状态", DataType: "tinyint", Operators: []string{"EQ", "NEQ", "IN", "NOT_IN"}},
			{Field: "dept_id", Label: "所属部门", DataType: "bigint", Operators: []string{"EQ", "NEQ", "IN", "NOT_IN"}},
		},
	})
}
