package pubhttp

import (
	"net/http"
	"strings"

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

	// 产物 SEO 体检（审计 SEO-019）：同步跑一次并返回结论。
	//
	// 同步而不是异步任务：体检只读产物文件、不写库、不出网，一个中型站点几百份 HTML
	// 的解析在毫秒级 —— 引入任务队列只会让「点了按钮没反应」成为新的排查对象。
	// 权限点单独给（seo:audit），与「改 URL」「发布」分开：体检查出问题的人未必有权改。
	g.POST("/seo-audit", builtin.CasbinMiddlewareForPath("/api/seo/audit"), func(c *gin.Context) {
		projectID := strings.TrimSpace(c.PostForm("project"))
		if projectID == "" {
			projectID = strings.TrimSpace(c.Query("project"))
		}
		issues, scanned, aerr := svc.RunSEOAudit(c.Request.Context(), projectID)
		if aerr != nil {
			response.ErrorWithMessage(c, http.StatusBadRequest, aerr.Error())
			return
		}
		response.Success(c, gin.H{"scanned": scanned, "issues": issues, "count": len(issues)})
	})
	return svc
}
