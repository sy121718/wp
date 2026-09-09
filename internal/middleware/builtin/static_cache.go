package builtin

// static_cache.go — 静态资源缓存策略（docs/09 §3 拆分后修复）。
//
// 背景：工作台前端拆成 ES modules 后，子模块 import 路径不带版本参数
//（/static/js/workbench/core.js 等 URL 恒定）。此前 /static 未下发任何
// Cache-Control，浏览器按 Last-Modified 启发式缓存，导致「改了 JS 但浏览器
// 仍执行旧模块」——表现为检查器面板空白/交互失效，硬刷新才恢复。
//
// 策略：统一 no-cache（配合 Last-Modified/ETag 协商缓存）——
// 浏览器每次发条件请求，未变更返回 304（开销极小），变更立即拿到新文件。
// 生产环境若要长缓存，应在构建期给文件名加内容哈希，而不是依赖这里放宽。

import "github.com/gin-gonic/gin"

// StaticCacheMiddleware 为静态资源下发协商缓存头。
func StaticCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")
		c.Next()
	}
}
