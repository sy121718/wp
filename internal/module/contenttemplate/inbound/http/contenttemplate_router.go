package contenttemplatehttp

// contenttemplate_router.go — contenttemplate 模块路由自装配（0-A2）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupContentTemplateRoutes 装配 contenttemplate 模块路由，返回模块契约。
func SetupContentTemplateRoutes(rg *gin.RouterGroup, db *gorm.DB) contenttemplatecontract.ContentTemplateService {
	svc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db))
	handle := NewHandle(svc)

	g := rg.Group("/contenttemplate")
	g.POST("/create", handle.Create)
	g.POST("/update", handle.Update)
	g.GET("/get", handle.Get)
	g.GET("/list", handle.List)
	return svc
}
