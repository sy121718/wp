package projecthttp

import (
	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

func SetupProjectRoutes(rg *permission.RouteGroup, db *gorm.DB, dict sysconfigcontract.DictReader) projectcontract.ProjectService {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	handle := NewHandle(svc)

	SetupThemeRoutes(rg, db)
	g := rg.Group("/project", builtin.SessionAuthMiddleware())
	g.POST("/create", permission.ProjectCreate, handle.Create)
	g.GET("/list", permission.ProjectList, handle.List)
	g.GET("/detail", permission.ProjectDetail, handle.Detail)
	g.POST("/update", permission.ProjectUpdate, handle.Update)
	return svc
}

func SetupThemeRoutes(rg *permission.RouteGroup, db *gorm.DB) {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	h := &ThemeHandle{svc: svc}

	g := rg.Group("/theme", builtin.SessionAuthMiddleware())
	g.GET("/list", permission.ProjectThemeList, h.List)
	g.POST("/create", permission.ProjectThemeCreate, h.Create)
	g.POST("/update", permission.ProjectThemeUpdate, h.Update)
	g.POST("/activate", permission.ProjectThemeActivate, h.Activate)
	g.POST("/delete", permission.ProjectThemeDelete, h.Delete)
	g.GET("/active", permission.ProjectThemeActive, h.Active)
}
