package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_insight.go - 文案与统计页路由：文案词条、访问统计、SEO 控制台。

func setupI18nRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 文案词条页（审计 I18N-003）：列表 / 筛选 / 编辑 / 新增 / 删除。
	// 此前词条只能靠迁移改 —— 能改文案的人只有写代码的人，运营遇到错别字只能等发版。
	// 读页面不挂 Casbin（与其它只读页一致），写操作挂 i18n:manage ——
	// 页面组本身只有 Session + CSRF，漏挂权限点等于任何登录管理员都能改全站文案。
	i18nPages := NewI18nEntryPageHandle()
	adminPages.GET("/i18n", i18nPages.I18nEntriesPage)
	adminPages.POST("/i18n/save", builtin.CasbinMiddlewareForPath("/api/i18n/save"), i18nPages.I18nEntrySave)
	adminPages.POST("/i18n/delete", builtin.CasbinMiddlewareForPath("/api/i18n/save"), i18nPages.I18nEntryDelete)
}

func setupInsightRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 访问统计（BIZ-8）：只读报表页（按天 / 按路径聚合 + 时间范围筛选 + 分页）。
	// 页面组已有 Session + CSRF；这里没有写操作，因此不挂 CasbinMiddlewareForPath ——
	// 权限点 analytics:view 用在菜单过滤与只读 API 的 Casbin 策略上。
	analyticsPages := NewAnalyticsPageHandle(d.analytics, d.projects)
	adminPages.GET("/analytics", analyticsPages.AnalyticsPage)

	// SEO 控制台（SEO-020）：复用 analytics 热门路径与 publication 已有体检端点。
	// 来源排行、sitemap/feed 状态尚无只读契约，页面明确显示不可用，不读取模块内部实现。
	seoPages := NewSEOPageHandle(d.analytics, d.projects)
	adminPages.GET("/seo", seoPages.SEOPage)
}
