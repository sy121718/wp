package analyticshttp

import (
	pageview "go_wp/pkg/view"
	"net/http"

	"github.com/gin-gonic/gin"

	mw "go_wp/pkg/middleware"

	"go_wp/internal/adminui"
	analyticscontract "go_wp/internal/module/analytics/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const SEOPageTemplate = "admin/analytics/seo.html"

const AnalyticsPageTemplate = "admin/analytics/analytics.html"

func setupAnalyticsPageRoutes(pages *gin.RouterGroup,
	analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	analyticsPages := NewAnalyticsPageHandle(analytics, projects)
	pages.GET("/analytics",
		mw.PageAuthz("/api/analytics/summary"),
		pageview.Slot(AnalyticsPageTemplate, adminui.RenderPage),
		analyticsPages.AnalyticsPage)

	seoPages := NewSEOPageHandle(analytics, projects)

	pages.GET("/seo",
		mw.PageAuthzAs("/api/seo/audit", http.MethodPost),
		pageview.Slot(SEOPageTemplate, adminui.RenderPage),
		seoPages.SEOPage)
}
