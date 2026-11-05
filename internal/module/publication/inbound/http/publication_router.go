package pubhttp

import (
	"net/http"

	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/internal/middleware/builtin"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/logger"
	mw "go_wp/pkg/middleware"
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

func SetupPublicationRoutes(rg *mw.RouteGroup, db *gorm.DB) pubcontract.PublicationService {
	svc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	g := rg.Group("/publication", builtin.SessionAuthMiddleware())

	g.GET("/receipts/pending", pubenums.PublicationReceiptsPending, PublicationPendingReceipts(svc))

	mw.DeclareRoute(mw.RouteSpec{
		Method: http.MethodPost, Path: "/api/seo/audit",
		Perm: adminenums.SEOAudit, Kind: mw.RoutePage,
		AuthObject: "/api/seo/audit", AuthMethod: http.MethodPost,
	})
	g.POST("/seo-audit", pubenums.PublicationSEOAudit, builtin.CasbinMiddlewareForPath("/api/seo/audit"),
		pageview.HTMLFragment, NewSeoAuditHandle(svc.RunSEOAudit).SeoAudit)
	return svc
}
