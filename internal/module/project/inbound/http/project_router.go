package projecthttp

import (
	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	mw "go_wp/pkg/middleware"

	"gorm.io/gorm"
)

func SetupProjectRoutes(rg *mw.RouteGroup, db *gorm.DB, dict sysconfigcontract.DictReader) projectcontract.ProjectService {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	handle := NewHandle(svc)

	SetupThemeRoutes(rg, db)
	g := rg.Group("/project", builtin.SessionAuthMiddleware())
	g.POST("/create", projectenums.ProjectCreate, handle.Create)
	g.GET("/list", projectenums.ProjectList, handle.List)
	g.GET("/detail", projectenums.ProjectDetail, handle.Detail)
	g.POST("/update", projectenums.ProjectUpdate, handle.Update)
	return svc
}

func SetupThemeRoutes(rg *mw.RouteGroup, db *gorm.DB) {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	h := &ThemeHandle{svc: svc}

	g := rg.Group("/theme", builtin.SessionAuthMiddleware())
	g.GET("/list", projectenums.ProjectThemeList, h.List)
	g.POST("/create", projectenums.ProjectThemeCreate, h.Create)
	g.POST("/update", projectenums.ProjectThemeUpdate, h.Update)
	g.POST("/activate", projectenums.ProjectThemeActivate, h.Activate)
	g.POST("/delete", projectenums.ProjectThemeDelete, h.Delete)
	g.GET("/active", projectenums.ProjectThemeActive, h.Active)
}
