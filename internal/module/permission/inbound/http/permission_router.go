package permissionhttp

import (
	"go_wp/internal/middleware/builtin"
	permissioncontract "go_wp/internal/module/permission/contract"
	permissionmodel "go_wp/internal/module/permission/model"
	permissionservice "go_wp/internal/module/permission/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupResult 包含 permission 模块对外暴露的契约实例。
type SetupResult struct {
	Service permissioncontract.PermissionService
}

// SetupPermissionRoutes 注册权限点模块路由。
// 返回 SetupResult，供顶层 routes.go 传递给依赖 permission 的模块（menu/role/admin）。
func SetupPermissionRoutes(rg *gin.RouterGroup, db *gorm.DB) SetupResult {
	m := permissionmodel.NewPermissionModel(db)
	svc := permissionservice.NewService(m)
	handle := NewHandle(svc)

	if rg != nil {
		g := rg.Group("/permission").Use(
			builtin.JWTAuthMiddleware(),
			builtin.CasbinMiddleware(),
		)
		{
			g.GET("/list", handle.List)
			g.GET("/detail", handle.Detail)
			g.GET("/options", handle.Options)
			g.POST("/create", handle.Create)
			g.POST("/update", handle.Update)
			g.POST("/delete", handle.Delete)
		}
	}

	return SetupResult{Service: svc}
}
