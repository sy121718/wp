package projecthttp

import (
	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupProjectRoutes 自装配 project 模块并注册路由，返回契约供 page/build 等模块依赖。
func SetupProjectRoutes(rg *permission.RouteGroup, db *gorm.DB) projectcontract.ProjectService {
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
