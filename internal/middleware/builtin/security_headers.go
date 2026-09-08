package builtin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/unrolled/secure"
)

// securityHeaders unrolled/secure 实例。
// Options 不可变且 Process 无状态（未启用 CSP nonce），包级单例可并发复用。
var securityHeaders = secure.New(secure.Options{
	// X-Frame-Options: SAMEORIGIN —— 不得用 DENY：构建器/预览存在同源 iframe
	// 嵌入场景，CustomFrameOptionsValue 直接指定值，不启用 FrameDeny（DENY）。
	CustomFrameOptionsValue: "SAMEORIGIN",
	// X-Content-Type-Options: nosniff —— 禁止浏览器 MIME 嗅探。
	ContentTypeNosniff: true,
	// Referrer-Policy —— 跨源引用只暴露 origin，不泄漏完整 URL（含查询参数）。
	ReferrerPolicy: "strict-origin-when-cross-origin",
	// Content-Security-Policy —— 允许同源 + CDN 脚本 + 内联样式/脚本。
	ContentSecurityPolicy: "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob: https:; " +
		"font-src 'self' data:; " +
		"object-src 'none'; " +
		"frame-ancestors 'self'; " +
		"base-uri 'self'",
})

// SecurityHeadersMiddleware 安全响应头中间件（unrolled/secure 封装）。
//
// 追加的响应头：
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: SAMEORIGIN（允许构建器 iframe 同源嵌入，不得用 DENY）
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - Content-Security-Policy：允许同源 + CDN 脚本 + 内联样式/脚本
//
// X-Powered-By 清理：Gin 与项目模板均不主动下发该头，此处做双阶段防御性
// 删除（进链前 + 全链返回后），保证无论哪一层（中间件/模板/未来引入的库）
// 意外设置都会在响应写出前被移除，不对外暴露技术栈。
//
// HTMX 与 /admin 兼容性：HTMX 请求是普通 HTML 请求（HX-Request 头），
// 不受 X-Frame-Options / Referrer-Policy 影响，CSP 对 AJAX 片段响应无额外
// 约束；/admin 页面与构建器 iframe 为同源嵌入，SAMEORIGIN 与
// frame-ancestors 'self' 均放行，交互不受影响。
//
// CSP 'unsafe-inline' 评估结论（保留，不可去除）：
//   - 后台模板存在多处内联 <script>：admin/login.html、admin/layout.html 的主题
//     初始化与切换脚本（localStorage 读取 + data-theme 写入，须在 CSS 前执行，
//     无法外置为文件）；去除 'unsafe-inline' 会直接破坏主题加载与切换。
//   - HTMX 2.x 依赖内联事件绑定（hx-on 属性等），去除后会静默失效。
//   - TinyMCE 经 cdn.jsdelivr.net/npm/tinymce@7 引入（已在 script-src 白名单，
//     非 cdn.tiny.cloud），不受影响。
//   - workbench/layout.html 的 <script type="application/json"> 为数据块，
//     不执行、不受 script-src 约束。
//     后续若将主题脚本外置为静态文件，可收窄为 'unsafe-inline' 仅保留在 style-src，
//     并把 script-src 改为 nonce/hash 方案。
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 进链前先清一次，拦截先前中间件的残留设置。
		c.Writer.Header().Del("X-Powered-By")

		// Process 写入上述安全头；当前配置未启用任何中断型检查
		// （SSLRedirect / AllowedHosts 等），err 恒为 nil，此分支为防御性兜底。
		if err := securityHeaders.Process(c.Writer, c.Request); err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()

		// 全链返回后再清一次：此时 handler 链已执行完毕、响应头尚未写出，
		// 任何一层设置的 X-Powered-By 都会被移除。
		c.Writer.Header().Del("X-Powered-By")
	}
}
