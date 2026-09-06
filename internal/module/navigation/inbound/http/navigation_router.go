// Package navigationhttp navigation 模块 HTTP 入口（0-C）。
// 路由挂载在 authorizedAPI（SessionAuth + CSRF + Casbin）之下，由顶层 routers.go 装配。
package navigationhttp

import (
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupNavigationRoutes 装配 navigation 模块路由，返回模块契约。
func SetupNavigationRoutes(rg *gin.RouterGroup, db *gorm.DB) navigationcontract.NavigationService {
	svc := navigationservice.NewService(navigationmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/navigation")
	g.POST("/create", handle.Create)
	g.POST("/update", handle.Update)
	g.GET("/get", handle.Get)
	g.GET("/list", handle.List)
	g.POST("/delete", handle.Delete)
	return svc
}
