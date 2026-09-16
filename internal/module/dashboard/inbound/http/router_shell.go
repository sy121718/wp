package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_shell.go - 后台外壳路由：登录页与工作台页面组（Session + CSRF + 权限上下文）。

func setupShellRoutes(router *gin.Engine, d *routeDeps, handle *Handle) {
	// 登录页：不挂认证（未登录请求被中间件 302 到此，独立布局渲染登录表单）。
	router.GET("/admin/login", handle.LoginPage)

	// 页面路由（全部挂 Session 认证：未登录的页面请求由中间件 302 到 /admin/login）。
	// 组级再挂 CSRFMiddleware：GET 直接放行，仅保护 POST /workbench/preview（草稿预览渲染）。
	// 前端刷新画布用原生表单 POST 提交（workbench.js refreshCanvas），已带 csrf_token 隐藏域。
	authPages := router.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), permContextMiddleware(d.authz))
	authPages.GET("/", handle.Dashboard)
	authPages.GET("/workbench", handle.Workbench)
	authPages.GET("/workbench/preview", handle.Preview)
	authPages.POST("/workbench/preview", handle.PreviewDraft)
	// 检查器面板片段（HTMX 化打样，docs/09 §3）：schema → 表单 HTML 由服务端渲染。
	authPages.POST("/workbench/inspector", handle.InspectorPanel)
	// 结构树片段（HTMX 化）：树 HTML 由服务端渲染，客户端只做一次事件委托。
	authPages.POST("/workbench/outline", handle.OutlineTree)
	// 页面设置面板与评分区（HTMX 化）：表单与评分均由服务端渲染。
	authPages.POST("/workbench/settings", handle.SettingsPanel)
	authPages.POST("/workbench/seo-score-panel", handle.SeoScorePanel)
	// 全局设置面板（站点主题字段）：字段表与渲染由服务端提供。
	authPages.POST("/workbench/global", handle.GlobalPanel)
	// 修订历史列表与恢复（HTMX 化）：列表由服务端渲染，恢复走服务端覆盖保存。
	authPages.POST("/workbench/history", handle.HistoryPanel)
	// 恢复修订会覆盖页面草稿（属写操作），必须做 Casbin 鉴权：
	// 权限点复用「保存草稿」（与 /admin/page/translations/save 同源），
	// 否则任何仅登录后台的低权限用户都能覆盖任意页面草稿。
	authPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), handle.HistoryRestore)
	// SEO 评分：只读分析草稿，返回评分与逐项建议。
	authPages.POST("/workbench/seo-score", handle.SEOScore)
	// 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
	authPages.GET("/workbench/block/preview", handle.BlockPreview)
	// 内容模板画布预览（EDT-001）：样例实体 + 模板 AST，GET 已保存 / POST 未保存草稿。
	authPages.GET("/workbench/template/preview", handle.TemplatePreview)
	authPages.POST("/workbench/template/preview", handle.TemplatePreviewDraft)
}
