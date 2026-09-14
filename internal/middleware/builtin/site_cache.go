package builtin

// site_cache.go — 访问面静态产物缓存策略（PERF-007）。
//
// 产物按 URL 激活后字节不可变（同 hash 重建才变），但 URL 会随发布切换指向新 hash，
// 因此 HTML 用 must-revalidate：CDN/浏览器可缓存但每次使用前要再验证。
// http.FileServer 已带 Last-Modified/ETag 协商，本中间件只补明确的 Cache-Control。

import "github.com/gin-gonic/gin"

// SiteCacheMiddleware 为 /site 访问面下发协商缓存头。
func SiteCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=0, must-revalidate")
		c.Next()
	}
}
