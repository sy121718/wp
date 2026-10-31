package analyticshttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

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
		adminui.PageAuthz("/api/analytics/summary"),
		adminui.PageSlot(AnalyticsPageTemplate),
		analyticsPages.AnalyticsPage)

	seoPages := NewSEOPageHandle(analytics, projects)

	pages.GET("/seo",
		adminui.PageAuthzAs("/api/seo/audit", http.MethodPost),
		adminui.PageSlot(SEOPageTemplate),
		seoPages.SEOPage)
}
