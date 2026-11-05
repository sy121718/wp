package buildhttp

import (
	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"

	buildcontract "go_wp/internal/module/build/contract"
	buildenums "go_wp/internal/module/build/enums"
	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	mw "go_wp/pkg/middleware"
)

func SetupBuildRoutes(rg *mw.RouteGroup, db *gorm.DB) buildcontract.BuildService {
	svc := buildservice.NewService(buildmodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/build", builtin.SessionAuthMiddleware())
	g.GET("/queue", buildenums.BuildQueue, handle.Queue)
	g.GET("/jobs", buildenums.BuildJobs, handle.List)
	g.POST("/retry", buildenums.BuildRetry, handle.Retry)
	return svc
}
