package builtin

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// rateLimitEntry 限流记录，记录窗口内的请求计数和重置时刻。
type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

var (
	rateLimitMu    sync.Mutex
	rateLimitStore = map[string]rateLimitEntry{}
)

// RequestRateLimitMiddleware 固定窗口限流中间件。
//
// 限流策略：
//   - 按 key（客户端 IP + 请求路径）维度计数
//   - 每个窗口结束后自动重置计数
//   - 超过限制的请求返回 429 Too Many Requests
//
// 参数：
//   - limit：每个时间窗口内允许的最大请求数
//   - window：时间窗口长度（如 time.Minute）
//
// 注意：rateLimitStore 是进程内内存存储，重启会丢失计数。
// 如需分布式限流，应替换为 Redis 实现。
//
// 适用位置：可以全局挂载，也可以只在敏感路由（登录、注册）挂载。
func RequestRateLimitMiddleware(limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limit <= 0 || window <= 0 {
			c.Next()
			return
		}

		// 静态资源请求不执行数据库或业务逻辑，不应消耗业务接口的限流额度。
		if isStaticAssetRequest(c) {
			c.Next()
			return
		}

		key := buildRateLimitKey(c)
		now := time.Now()

		rateLimitMu.Lock()
		entry, exists := rateLimitStore[key]
		if !exists || !entry.resetAt.After(now) {
			entry = rateLimitEntry{
				count:   0,
				resetAt: now.Add(window),
			}
		}
		entry.count++
		rateLimitStore[key] = entry
		rateLimitMu.Unlock()

		if entry.count > limit {
			logger.Scene("middleware").With("key", key).Warn("请求被限流")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, response.Response{
				Code:    http.StatusTooManyRequests,
				Message: "请求过于频繁",
			})
			return
		}

		c.Next()
	}
}

func isStaticAssetRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/storage/")
}

// buildRateLimitKey 根据请求构建限流 key。
//
// 格式：clientIP|请求路径
// 优先使用 Gin 的 FullPath（注册路径，含 :param 占位符），
// 取不到时回退到 URL.RawPath。
func buildRateLimitKey(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "unknown"
	}
	path := strings.TrimSpace(c.FullPath())
	if path == "" {
		path = strings.TrimSpace(c.Request.URL.Path)
	}
	return c.ClientIP() + "|" + path
}

// ResetRateLimitStore 清空限流计数存储。
// 主要用于测试场景，确保每个测试用例从干净的计数开始。
func ResetRateLimitStore() {
	rateLimitMu.Lock()
	defer rateLimitMu.Unlock()
	rateLimitStore = map[string]rateLimitEntry{}
}
