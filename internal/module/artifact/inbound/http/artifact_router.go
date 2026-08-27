package artifacthttp

import (
	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupArtifactRoutes 自装配 artifact 模块（产物元数据查询接口）。
func SetupArtifactRoutes(rg *gin.RouterGroup, db *gorm.DB) artifactcontract.ArtifactService {
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	g := rg.Group("/artifact")
	g.GET("/detail", func(c *gin.Context) {})
	return svc
}
