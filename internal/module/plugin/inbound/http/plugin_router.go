package pluginhttp

// plugin_router.go — 插件模块路由自装配（docs/06-plugin-system.md）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）；权限点 seed 见
// public/migrations/032_plugin_permissions.sql。

import (
	admincontract "go_wp/internal/module/admin/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupPluginRoutes 装配插件模块路由，返回模块契约（dashboard/page 装配用）。
//
// adminAuthz：admin 模块对外暴露的权限上下文查询服务。插件作为外部插件宿主，
// 需要在插件运行时经 admin 契约读取当前用户权限，因此由顶层装配从
// adminhttp.SetupAdminRoutes 取回并注入。
func SetupPluginRoutes(rg *permission.RouteGroup, db *gorm.DB, adminAuthz admincontract.AuthzContextService) plugincontract.PluginService {
	m := pluginmodel.NewModel(db)
	svc := pluginservice.NewService(m)
	svc.SetAdminAuthz(adminAuthz)

	// 插件三处产物（L1 schema / 注册行 / 存储目录）的定时对账巡检。
	// 卸载的三步里有两处能同事务、但存储目录是跨库动作（故意忽略错误），因此每一步都可能
	// 单独失败留下残片；而残片没有任何接口能发现（孤儿 schema 里可能装着真实业务数据、
	// 缺 schema 会让构建装配在建表时炸、缺目录会让装配静默跳过该插件）。
	// 巡检**只报告不清理**；装配在这里启动一次，每个进程只起一个 goroutine。
	pluginservice.StartPluginPatrolScheduler(svc)

	handle := NewHandle(svc)

	g := rg.Group("/plugin")
	g.POST("/install", permission.PluginInstall, handle.Install)
	g.GET("/list", permission.PluginList, handle.List)
	g.POST("/toggle", permission.PluginToggle, handle.Toggle)
	g.POST("/uninstall", permission.PluginUninstall, handle.Uninstall)
	g.GET("/detail", permission.PluginDetail, handle.Detail)
	return svc
}
