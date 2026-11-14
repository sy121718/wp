package analyticshttp

import (
	pageview "go_wp/pkg/view"
	"net/http"

	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
)

const SEOPageTemplate = "admin/analytics/seo.html"

const AnalyticsPageTemplate = "admin/analytics/analytics.html"

func setupAnalyticsPageRoutes(reg *mw.Registry, pages *gin.RouterGroup,
	analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	analyticsPages := NewAnalyticsPageHandle(analytics, projects)
	pages.GET("/analytics",
		mw.PageAuthz("/api/analytics/summary"),
		pageview.Slot(AnalyticsPageTemplate, pageview.RenderPage),
		analyticsPages.AnalyticsPage)
	mw.DeclarePageGetOn(reg, "/admin/analytics", "/api/analytics/summary")

	seoPages := NewSEOPageHandle(analytics, projects)

	pages.GET("/seo",
		mw.PageAuthzAs("/api/seo/audit", http.MethodPost),
		pageview.Slot(SEOPageTemplate, pageview.RenderPage),
		seoPages.SEOPage)
	mw.DeclarePageGetAsOn(reg, "/admin/seo", "/api/seo/audit", http.MethodPost)
}
