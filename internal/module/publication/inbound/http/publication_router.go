package pubhttp

import (
	"net/http"

	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPublicationRoutes 自装配 publication 模块（路由占用由 page 模块经契约调用）。
func SetupPublicationRoutes(rg *gin.RouterGroup, db *gorm.DB) pubcontract.PublicationService {
	svc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	g := rg.Group("/publication", builtin.SessionAuthMiddleware())
	// 占位路由：前端可能探测该端点，但接口尚未实现。
	// 明确返回 501 而非 200 空体，避免调用方误判成功（审计 Low：假 handler）。
	g.GET("/receipts/pending", func(c *gin.Context) {
		response.ErrorWithMessage(c, http.StatusNotImplemented, "接口未实现：待处理回执查询暂未提供")
	})
	return svc
}
