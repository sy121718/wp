package pluginhttp

// plugin_router.go — 插件模块路由自装配（docs/06-plugin-system.md）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）；权限点 seed 见
// public/migrations/032_plugin_permissions.sql。

import (
	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPluginRoutes 装配插件模块路由，返回模块契约（dashboard/page 装配用）。
//
// adminAuthz：admin 模块对外暴露的权限上下文查询服务。插件作为外部插件宿主，
// 需要在插件运行时经 admin 契约读取当前用户权限，因此由顶层装配从
// adminhttp.SetupAdminRoutes 取回并注入。
func SetupPluginRoutes(rg *gin.RouterGroup, db *gorm.DB, adminAuthz admincontract.AuthzContextService) plugincontract.PluginService {
	m := pluginmodel.NewModel(db)
	svc := pluginservice.NewService(m)
	svc.SetAdminAuthz(adminAuthz)
	handle := NewHandle(svc)

	g := rg.Group("/plugin")
	g.POST("/install", handle.Install)
	g.GET("/list", handle.List)
	g.POST("/toggle", handle.Toggle)
	g.POST("/uninstall", handle.Uninstall)
	g.GET("/detail", handle.Detail)
	return svc
}
