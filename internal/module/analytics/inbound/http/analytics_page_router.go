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
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/analytics/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
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
	pages.GET("/analytics", shell.PageAuthz("/api/analytics/summary"), analyticsPages.AnalyticsPage)

	seoPages := NewSEOPageHandle(analytics, projects)
	// SEO 控制台的 obj 是它的体检权限点 seo:audit（菜单 /admin/seo 绑的就是它），
	// 而该权限点声明为 POST /api/seo/audit —— 页面是 GET，故用 As 变体显式指定 act。
	// 存量口径不一致（页面 GET 借一个 POST 权限点），归一与 02-Z 第二刀同批。
	pages.GET("/seo", shell.PageAuthzAs("/api/seo/audit", http.MethodPost), seoPages.SEOPage)
}
