package navigationhttp

import (
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

func SetupNavigationRoutes(rg *permission.RouteGroup, db *gorm.DB, projects projectcontract.ProjectService) (navigationcontract.NavigationService, *Handle) {
	svc := navigationservice.NewService(navigationmodel.NewModel(db), projects)
	handle := NewHandle(svc)

	g := rg.Group("/navigation")
	g.POST("/create", permission.NavigationCreate, handle.Create)
	g.POST("/update", permission.NavigationUpdate, handle.Update)
	g.GET("/get", permission.NavigationGet, handle.Get)
	g.GET("/list", permission.NavigationList, handle.List)
	g.POST("/delete", permission.NavigationDelete, handle.Delete)
	return svc, handle
}
