package adminhttp

import (
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminmodel "go_wp/internal/module/admin/model"
	adminservice "go_wp/internal/module/admin/service"
	menucontract "go_wp/internal/module/menu/contract"
	permissioncontract "go_wp/internal/module/permission/contract"
	rolecontract "go_wp/internal/module/role/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupResult 管理员模块装配结果，供顶层 routes 获取 Service 契约。
type SetupResult struct {
	Service admincontract.AdminService
}

func SetupAdminRoutes(
	rg *gin.RouterGroup,
	db *gorm.DB,
	menuSvc menucontract.MenuService,
	roleSvc rolecontract.RoleService,
	permSvc permissioncontract.PermissionService,
) SetupResult {
	if rg == nil {
		return SetupResult{}
	}

	m := adminmodel.NewAdminModel(db)
	svc := adminservice.NewService(m, menuSvc, roleSvc, permSvc)
	handle := NewHandle(svc)

	admin := rg.Group("/admin")
	admin.POST("/login", handle.Login)

	auth := admin.Group("").Use(builtin.JWTAuthMiddleware(), builtin.DataRuleContextMiddleware())
	{
		auth.POST("/logout", handle.Logout)
		auth.GET("/profile", handle.Profile)
		auth.GET("/routes", handle.Routes)
	}

	authorized := admin.Group("").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
		builtin.DataRuleContextMiddleware(),
	)
	{
		authorized.GET("/list", handle.List)
		authorized.GET("/detail", handle.Detail)
		authorized.POST("/create", handle.Create)
		authorized.POST("/edit", handle.Edit)
		authorized.POST("/delete", handle.Delete)
		authorized.GET("/role/list", handle.RoleList)
		authorized.POST("/role/save", handle.RoleSave)
		authorized.GET("/menu/list", handle.MenuList)
		authorized.POST("/menu/save", handle.MenuSave)
	}

	return SetupResult{Service: svc}
}
