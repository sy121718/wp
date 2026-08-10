package mediahttp

import (
	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
	mediaservice "go_wp/internal/module/media/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupMediaRoutes 注册媒体模块路由，返回契约接口供其他模块引用。
func SetupMediaRoutes(rg *gin.RouterGroup, db *gorm.DB) mediacontract.MediaService {
	am := mediamodel.NewAttachmentModel(db)
	cm := mediamodel.NewFileCategoryModel(db)
	svc := mediaservice.NewService(am, cm)
	handle := NewHandle(svc)

	g := rg.Group("/media")
	{
		g.POST("/upload", handle.Upload)
		g.GET("/list", handle.List)
		g.GET("/detail", handle.Detail)
		g.POST("/delete", handle.Delete)
		g.GET("/category/tree", handle.CategoryTree)
	}
	return svc
}