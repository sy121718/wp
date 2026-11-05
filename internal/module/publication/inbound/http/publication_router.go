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
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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

func SetupPublicationRoutes(rg *permission.RouteGroup, db *gorm.DB) pubcontract.PublicationService {
	svc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	g := rg.Group("/publication", builtin.SessionAuthMiddleware())

	g.GET("/receipts/pending", permission.PublicationReceiptsPending, PublicationPendingReceipts(svc))

	permission.Declare(http.MethodPost, "/api/seo/audit", permission.SEOAudit)
	g.POST("/seo-audit", permission.PublicationSEOAudit, builtin.CasbinMiddlewareForPath("/api/seo/audit"),
		pageview.HTMLFragment, NewSeoAuditHandle(svc.RunSEOAudit).SeoAudit)
	return svc
}
