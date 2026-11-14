package pluginhttp

import (
	"context"
	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

// SetupPluginRoutes 装配插件路由；ctx 是进程级上下文，巡检循环挂在它上面。
// 返回的 stop 取消循环并等待在途一轮结束，调用方须在关闭数据库之前调用它。
func SetupPluginRoutes(ctx context.Context, rg *mw.RouteGroup, db *gorm.DB, adminAuthz admincontract.AuthzContextService) (plugincontract.PluginService, func()) {
	m := pluginmodel.NewModel(db)
	svc := pluginservice.NewService(m)
	svc.SetAdminAuthz(adminAuthz)

	stopPatrol := pluginservice.StartPluginPatrolScheduler(ctx, svc)

	handle := NewHandle(svc)

	g := rg.Group("/plugin")
	g.POST("/install", pluginenums.PluginInstall, handle.Install)
	g.GET("/list", pluginenums.PluginList, handle.List)
	g.POST("/toggle", pluginenums.PluginToggle, pageview.HTMLFragment, handle.Toggle)
	g.POST("/uninstall", pluginenums.PluginUninstall, pageview.HTMLFragment, handle.Uninstall)
	g.GET("/detail", pluginenums.PluginDetail, handle.Detail)
	return svc, stopPatrol
}
