package pluginhttp

import (
	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

func SetupPluginRoutes(rg *mw.RouteGroup, db *gorm.DB, adminAuthz admincontract.AuthzContextService) plugincontract.PluginService {
	m := pluginmodel.NewModel(db)
	svc := pluginservice.NewService(m)
	svc.SetAdminAuthz(adminAuthz)

	pluginservice.StartPluginPatrolScheduler(svc)

	handle := NewHandle(svc)

	g := rg.Group("/plugin")
	g.POST("/install", pluginenums.PluginInstall, handle.Install)
	g.GET("/list", pluginenums.PluginList, handle.List)
	g.POST("/toggle", pluginenums.PluginToggle, pageview.HTMLFragment, handle.Toggle)
	g.POST("/uninstall", pluginenums.PluginUninstall, pageview.HTMLFragment, handle.Uninstall)
	g.GET("/detail", pluginenums.PluginDetail, handle.Detail)
	return svc
}
