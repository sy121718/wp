package pubhttp

import (
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPublicationRoutes 自装配 publication 模块（路由占用由 page 模块经契约调用）。
func SetupPublicationRoutes(rg *gin.RouterGroup, db *gorm.DB) pubcontract.PublicationService {
	svc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	g := rg.Group("/publication")
	g.GET("/receipts/pending", func(c *gin.Context) {})
	return svc
}
