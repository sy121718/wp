package analyticshttp

// analytics_page_router.go — 访问统计页与 SEO 控制台（/admin/analytics、/admin/seo）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 analytics_page.go / seo_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 本页没有写操作，页面 GET 的 Casbin 待补（见 docs/02-Z §4.3）—— 权限点 analytics:view
// 目前只用在菜单过滤与只读 API 的 Casbin 策略上。
// 函数保持**不导出**：装配层只调 SetupAnalyticsRoutes，页面注册由它在同一位置调用。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/module/analytics/contract"
	"go_wp/internal/module/project/contract"
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
