// Package rolehttp 角色模块 HTTP 层。
package rolehttp

import (
	"go_wp/internal/middleware/builtin"
	menucontract "go_wp/internal/module/menu/contract"
	permissioncontract "go_wp/internal/module/permission/contract"
	rolecontract "go_wp/internal/module/role/contract"
	rolemodel "go_wp/internal/module/role/model"
	roleservice "go_wp/internal/module/role/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupResult 角色模块装配结果，供顶层 routes 获取 Service 契约。
type SetupResult struct {
	Service rolecontract.RoleService
}

// SetupRoleRoutes 注册角色模块路由。
// permSvc 来自 permission，menuSvc 来自 menu。
func SetupRoleRoutes(
	rg *gin.RouterGroup,
	db *gorm.DB,
	permSvc permissioncontract.PermissionService,
	menuSvc menucontract.MenuService,
) SetupResult {
	m := rolemodel.NewRoleModel(db)
	svc := roleservice.NewService(m, menuSvc, permSvc)
	handle := NewHandle(svc)

	if rg != nil {
		g := rg.Group("/role").Use(
			builtin.JWTAuthMiddleware(),
			builtin.CasbinMiddleware(),
		)
		{
			g.GET("/list", handle.List)
			g.GET("/detail", handle.Detail)
			g.POST("/create", handle.Create)
			g.POST("/update", handle.Update)
			g.POST("/delete", handle.Delete)
			g.GET("/menu/list", handle.MenuList)
			g.POST("/menu/save", handle.MenuSave)
			g.GET("/user/list", handle.UserList)
			g.POST("/user/save", handle.UserSave)
		}
	}

	return SetupResult{Service: svc}
}
