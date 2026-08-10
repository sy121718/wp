package builtin

import "github.com/gin-gonic/gin"

// SecurityHeadersMiddleware 安全响应头中间件。
//
// 追加的响应头：
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: SAMEORIGIN（允许构建器 iframe 同源嵌入）
//   - Content-Security-Policy：允许同源 + CDN 脚本 + 内联样式/脚本
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
