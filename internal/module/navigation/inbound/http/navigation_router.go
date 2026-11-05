package navigationhttp

import (
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"

	"gorm.io/gorm"
)

func SetupNavigationRoutes(rg *mw.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) (navigationcontract.NavigationService, *Handle) {
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/navigation")
	// 一个端点一种确定协议：普通界面是标准表单，译文行是明确的 JSON 对象。
	g.POST("/create", navigationenums.NavigationCreate, pageview.HTMLFragment, handle.Create)
	g.POST("/update", navigationenums.NavigationUpdate, pageview.HTMLFragment, handle.Update)
	g.POST("/move", navigationenums.NavigationMove, pageview.HTMLFragment, handle.Move)
	g.POST("/sources", navigationenums.NavigationSourceAdd, pageview.HTMLFragment, handle.CreateFromSource)
	g.POST("/translations", navigationenums.NavigationTranslate, pageview.HTMLFragment, handle.UpdateTranslations)
	g.GET("/get", navigationenums.NavigationGet, handle.Get)
	g.GET("/list", navigationenums.NavigationList, handle.List)
	g.POST("/delete", navigationenums.NavigationDelete, pageview.HTMLFragment, handle.Delete)
	g.POST("/delete-bulk", navigationenums.NavigationDeleteBulk, pageview.HTMLFragment, handle.DeleteBulk)
	return svc, handle
}
