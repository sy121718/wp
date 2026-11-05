package navigationhttp

import (
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

func SetupNavigationRoutes(rg *permission.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) (navigationcontract.NavigationService, *Handle) {
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/navigation")
	// 一个端点一种确定协议：普通界面是标准表单，译文行是明确的 JSON 对象。
	g.POST("/create", permission.NavigationCreate, pageview.HTMLFragment, handle.Create)
	g.POST("/update", permission.NavigationUpdate, pageview.HTMLFragment, handle.Update)
	g.POST("/move", permission.NavigationMove, pageview.HTMLFragment, handle.Move)
	g.POST("/sources", permission.NavigationSourceAdd, pageview.HTMLFragment, handle.CreateFromSource)
	g.POST("/translations", permission.NavigationTranslate, pageview.HTMLFragment, handle.UpdateTranslations)
	g.GET("/get", permission.NavigationGet, handle.Get)
	g.GET("/list", permission.NavigationList, handle.List)
	g.POST("/delete", permission.NavigationDelete, pageview.HTMLFragment, handle.Delete)
	g.POST("/delete-bulk", permission.NavigationDeleteBulk, pageview.HTMLFragment, handle.DeleteBulk)
	return svc, handle
}
