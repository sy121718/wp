package pubhttp

import (
	"net/http"

	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/permission"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PublicationPendingReceipts 待处理回执查询 handler（导出以便集成测试直接调用，
// 理由同 artifact.ArtifactDetail：路由带 Session + Casbin）。
func PublicationPendingReceipts(svc pubcontract.PublicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := svc.ListPendingReceipts(c.Request.Context())
		if err != nil {
			logger.Scene("publication").With("path", c.Request.URL.Path).Error(err, "待处理回执查询失败")
			response.ErrorWithMessage(c, http.StatusInternalServerError, pubenums.ErrInternal)
			return
		}
		response.Success(c, gin.H{"items": list, "total": len(list)})
	}
}

// SetupPublicationRoutes 自装配 publication 模块（路由占用由 page 模块经契约调用）。
func SetupPublicationRoutes(rg *permission.RouteGroup, db *gorm.DB) pubcontract.PublicationService {
	svc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	g := rg.Group("/publication", builtin.SessionAuthMiddleware())
	// 待处理回执查询（FIX-24）：同样是「做完了、门忘了开」—— 实现与索引都在
	// （contract 的 ListPendingReceipts、service/publication_receipt_ledger.go、迁移 267 的专用索引）。
	// 它是「发布中断后有多少回执没收敛」的运维入口。
	g.GET("/receipts/pending", permission.PublicationReceiptsPending, PublicationPendingReceipts(svc))

	// 产物 SEO 体检（审计 SEO-019）：同步跑一次并返回结论。
	//
	// 同步而不是异步任务：体检只读产物文件、不写库、不出网，一个中型站点几百份 HTML
	// 的解析在毫秒级 —— 引入任务队列只会让「点了按钮没反应」成为新的排查对象。
	// 真实路由需要 publication:seo_audit，额外校验迁移 183 的 seo:audit；
	// 两条既有策略都必须命中，页面动作仍按 seo:audit 展示。
	// 这条路由上挂了**两条** Casbin 策略：声明式那条按真实路径（publication:seo_audit），
	// 中间件那条额外校验迁移 183 的 seo:audit（/api/seo/audit 是虚拟对象、没有独立路由）——
	// 显式声明后者，否则 permission.RoutesOf 查不到 seo:audit，AI 工具按 fail closed 一律 forbidden。
	permission.Declare(http.MethodPost, "/api/seo/audit", permission.SEOAudit)
	g.POST("/seo-audit", permission.PublicationSEOAudit, builtin.CasbinMiddlewareForPath("/api/seo/audit"), seoAuditHandler(svc.RunSEOAudit))
	return svc
}
