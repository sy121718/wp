// Package menuhttp 菜单模块 HTTP 层。
package menuhttp

import (
	"go_wp/internal/middleware/builtin"
	menucontract "go_wp/internal/module/menu/contract"
	menumodel "go_wp/internal/module/menu/model"
	menuservice "go_wp/internal/module/menu/service"
	permissioncontract "go_wp/internal/module/permission/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupResult 菜单模块装配结果，供顶层 routes 获取 Service 契约。
type SetupResult struct {
	Service menucontract.MenuService
}

// SetupMenuRoutes 注册菜单模块路由。
// permSvc 来自 permission 模块的契约。
func SetupMenuRoutes(rg *gin.RouterGroup, db *gorm.DB, permSvc permissioncontract.PermissionService) SetupResult {
	m := menumodel.NewMenuModel(db)
	svc := menuservice.NewService(m, permSvc)
	handle := NewHandle(svc)

	if rg != nil {
		g := rg.Group("/menu").Use(
			builtin.JWTAuthMiddleware(),
			builtin.CasbinMiddleware(),
		)
		{
			g.GET("/tree", handle.Tree)
			g.GET("/detail", handle.Detail)
			g.POST("/create", handle.Create)
			g.POST("/update", handle.Update)
			g.POST("/delete", handle.Delete)
		}
	}

	return SetupResult{Service: svc}
}
