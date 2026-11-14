package pubhttp

import (
	"net/http"

	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/internal/middleware/builtin"
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

	// 真实入口是 POST /api/publication/seo-audit（路由组内路径），Method/Path 必须写它；
	// 复用的策略对象是 /api/seo/audit，只出现在 AuthObject/AuthMethod 上。
	//
	// 两项既有检查同时保留：
	//   · router 登记里的 PublicationSEOAudit（本操作自身的权限点）；
	//   · Casbin 中间件按 /api/seo/audit 额外校验 SEO 审核对象。
	// 用 DeclareRouteIn 把「真实路由」和「鉴权目标」分开登记：Path 是真实入口
	// /api/publication/seo-audit，AuthObject 是复用的 /api/seo/audit 策略对象。
	g.DeclareRouteIn(http.MethodPost, "/seo-audit", mw.RouteSpec{
		Perm: pubenums.PublicationSEOAudit, Kind: mw.RouteProtected,
		AuthObject: "/api/seo/audit", AuthMethod: http.MethodPost,
	})
	g.RouterGroup.POST("/seo-audit", builtin.CasbinMiddlewareForPath("/api/seo/audit"),
		pageview.HTMLFragment, NewSeoAuditHandle(svc.RunSEOAudit).SeoAudit)
	return svc
}
