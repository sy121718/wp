// Package navigationhttp navigation 模块 HTTP 入口（0-C）。
// 路由挂载在 authorizedAPI（SessionAuth + CSRF + Casbin）之下，由顶层 routers.go 装配。
package navigationhttp

import (
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupNavigationRoutes 装配 navigation 模块路由，返回模块契约。
//
// projects 是站点工程契约：只带 id 的入口（更新 / 详情 / 删除）要逐工程探测工程归属，
// 而 navigations 带 FORCE 策略，作用域只能落到具体工程（DB-009）。
func SetupNavigationRoutes(rg *permission.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) navigationcontract.NavigationService {
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/navigation")
	g.POST("/create", permission.NavigationCreate, handle.Create)
	g.POST("/update", permission.NavigationUpdate, handle.Update)
	g.GET("/get", permission.NavigationGet, handle.Get)
	g.GET("/list", permission.NavigationList, handle.List)
	g.POST("/delete", permission.NavigationDelete, handle.Delete)
	return svc
}
