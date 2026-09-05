package builtin

import "github.com/gin-gonic/gin"

// SecurityHeadersMiddleware 安全响应头中间件。
//
// 追加的响应头：
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: SAMEORIGIN（允许构建器 iframe 同源嵌入）
//   - Content-Security-Policy：允许同源 + CDN 脚本 + 内联样式/脚本
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
//   后续若将主题脚本外置为静态文件，可收窄为 'unsafe-inline' 仅保留在 style-src，
//   并把 script-src 改为 nonce/hash 方案。
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob: https:; "+
				"font-src 'self' data:; "+
				"object-src 'none'; "+
				"frame-ancestors 'self'; "+
				"base-uri 'self'")
		c.Next()
	}
}
