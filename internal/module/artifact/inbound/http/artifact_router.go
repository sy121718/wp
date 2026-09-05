package artifacthttp

import (
	"net/http"

	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupArtifactRoutes 自装配 artifact 模块（产物元数据查询接口）。
func SetupArtifactRoutes(rg *gin.RouterGroup, db *gorm.DB) artifactcontract.ArtifactService {
	svc := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	g := rg.Group("/artifact", builtin.SessionAuthMiddleware())
	// 占位路由：前端可能探测该端点，但接口尚未实现。
	// 明确返回 501 而非 200 空体，避免调用方误判成功（审计 Low：假 handler）。
	g.GET("/detail", func(c *gin.Context) {
		response.ErrorWithMessage(c, http.StatusNotImplemented, "接口未实现：产物详情查询暂未提供")
	})
	return svc
}
