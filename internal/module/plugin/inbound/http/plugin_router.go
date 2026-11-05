package pluginhttp

import (
	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	"go_wp/internal/permission"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

func SetupPluginRoutes(rg *permission.RouteGroup, db *gorm.DB, adminAuthz admincontract.AuthzContextService) plugincontract.PluginService {
	m := pluginmodel.NewModel(db)
	svc := pluginservice.NewService(m)
	svc.SetAdminAuthz(adminAuthz)

	pluginservice.StartPluginPatrolScheduler(svc)

	handle := NewHandle(svc)

	g := rg.Group("/plugin")
	g.POST("/install", permission.PluginInstall, handle.Install)
	g.GET("/list", permission.PluginList, handle.List)
	g.POST("/toggle", permission.PluginToggle, pageview.HTMLFragment, handle.Toggle)
	g.POST("/uninstall", permission.PluginUninstall, pageview.HTMLFragment, handle.Uninstall)
	g.GET("/detail", permission.PluginDetail, handle.Detail)
	return svc
}
