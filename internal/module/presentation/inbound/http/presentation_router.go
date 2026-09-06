package presentationhttp

// presentation_router.go — presentation 模块路由自装配（0-A2）。

import (
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPresentationRoutes 装配 presentation 模块路由，返回模块契约。
func SetupPresentationRoutes(rg *gin.RouterGroup, db *gorm.DB,
	templates contenttemplatecontract.ContentTemplateService,
	content contentcontract.ContentService) presentationcontract.PresentationService {
	svc := presentationservice.NewService(presentationmodel.NewModel(db), templates, content)
	handle := NewHandle(svc)

	g := rg.Group("/presentation")
	g.POST("/create", handle.CreateInstance)
	g.POST("/rebuild", handle.Rebuild)
	g.GET("/get", handle.Get)
	g.GET("/list", handle.List)
	g.POST("/delete", handle.Delete)
	return svc
}
