package projecthttp

import (
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupProjectRoutes 自装配 project 模块并注册路由，返回契约供 page/build 等模块依赖。
func SetupProjectRoutes(rg *gin.RouterGroup, db *gorm.DB) projectcontract.ProjectService {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	handle := NewHandle(svc)

	g := rg.Group("/project")
	g.POST("/create", handle.Create)
	g.GET("/list", handle.List)
	g.GET("/detail", handle.Detail)
	g.POST("/update", handle.Update)
	return svc
}
