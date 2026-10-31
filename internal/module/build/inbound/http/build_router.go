package buildhttp

import (
	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/permission"

	buildcontract "go_wp/internal/module/build/contract"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
)

func SetupBuildRoutes(rg *permission.RouteGroup, db *gorm.DB) buildcontract.BuildService {
	svc := buildservice.NewService(buildmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/build", builtin.SessionAuthMiddleware())
	g.GET("/queue", permission.BuildQueue, handle.Queue)
	g.GET("/jobs", permission.BuildJobs, handle.List)
	g.POST("/retry", permission.BuildRetry, handle.Retry)
	return svc
}
