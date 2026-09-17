package analyticshttp

// analytics_page_router.go — 访问统计后台页路由（原 dashboard 的 router_insight.go 里的 analytics 一行）。
//
// 页面挂在装配层传入的 /admin 组上（Session + CSRF + 权限上下文已由装配层挂好）。
// 这里没有写操作，因此不挂 CasbinMiddlewareForPath —— 权限点 analytics:view 用在
// 菜单过滤与只读 API 的 Casbin 策略上。
// 注意：i18n 文案词条页仍归 dashboard，不随本次搬迁移动。

import (
	"github.com/gin-gonic/gin"

	analyticscontract "go_wp/internal/module/analytics/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// setupAnalyticsPageRoutes 注册访问统计页与 SEO 控制台（pages = /admin 页面组；
// pages 为 nil 时跳过）。SEO 控制台（SEO-020）读 analytics 热门路径 + project 站点清单，
// 与统计页同域相邻，故随本段一起注册；SEO 体检动作由 publication 现有端点执行。
func setupAnalyticsPageRoutes(pages *gin.RouterGroup,
	analytics analyticscontract.AnalyticsService, projects projectcontract.ProjectService) {
	if pages == nil {
		return
	}
	analyticsPages := NewAnalyticsPageHandle(analytics, projects)
	pages.GET("/analytics", analyticsPages.AnalyticsPage)

	seoPages := NewSEOPageHandle(analytics, projects)
	pages.GET("/seo", seoPages.SEOPage)
}
