package pluginhttp

// plugin_router.go — 插件模块路由自装配（docs/06-plugin-system.md）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）；权限点 seed 见
// public/migrations/032_plugin_permissions.sql。

import (
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPluginRoutes 装配插件模块路由，返回模块契约（dashboard/page 装配用）。
func SetupPluginRoutes(rg *gin.RouterGroup, db *gorm.DB) plugincontract.PluginService {
	m := pluginmodel.NewModel(db)
	svc := pluginservice.NewService(m)
	handle := NewHandle(svc)

	g := rg.Group("/plugin")
	g.POST("/install", handle.Install)
	g.GET("/list", handle.List)
	g.POST("/toggle", handle.Toggle)
	g.POST("/uninstall", handle.Uninstall)
	g.GET("/detail", handle.Detail)
	return svc
}
