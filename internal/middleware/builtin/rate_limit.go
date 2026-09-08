package builtin

import (
	"net/http"
	"strings"
	"time"

	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"github.com/gin-gonic/gin"
)

// rateLimitBucketTTL 限流器内单个 IP 令牌桶的存活时间。
// IP 停止访问超过该时长后其令牌桶被自动回收，防止长期不活跃的 key
// 在内存中无限累积（承担旧实现 sweep 清理的职责）。
const rateLimitBucketTTL = 10 * time.Minute

// RequestRateLimitMiddleware 按 IP 维度的令牌桶限流中间件（tollbooth 封装）。
//
// 限流策略：
//   - key 维度：客户端 IP，仅解析 RemoteAddr（与 release 模式 TrustedProxies=nil
//     的 ClientIP 语义一致，X-Forwarded-For / X-Real-IP 无法伪造绕过限流）
//   - 速率与突发容量：window 窗口内允许 limit 次（burst=limit，窗口额度即
//     突发容量），平均速率 limit/window 次/秒
//   - 超限返回 429 Too Many Requests，响应体为统一 Response JSON 结构
//
// 分级挂载惯例（构成登录防线）：
//   - /api/captcha（GET）与 /api/admin/login（POST）在各自模块路由内无条件挂载
//     严格限流（每 IP 每分钟 10 次），不依赖全局开关，防止匿名脚本爆破；
//   - 其余 /api/* 与页面路由由 middleware.Setup 全局挂基础限流
//     （默认每 IP 每分钟 120 次，由 server.rate_limit_enabled 控制），
//     页面路由获得宽松保护，静态资源（/static/、/storage/）豁免。
//
// 注意：限流状态为进程内令牌桶，重启即重置；多实例部署需外置存储。
// 解析不到客户端 IP 的请求（RemoteAddr 为空，生产环境不会出现）会被
// tollbooth 跳过限流，属 fail-open 语义；每次调用本函数都创建独立的
// 限流器实例（路由注册期一次性创建），实例之间计数互不影响，
// 测试无需手动清零。
func RequestRateLimitMiddleware(limit int, window time.Duration) gin.HandlerFunc {
	// 非法参数直接放行：不构造限流器。
	if limit <= 0 || window <= 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	// 限流器在路由注册期一次性创建，所有请求共享同一令牌桶状态；
	// 若在请求闭包内创建，每个请求都会拿到全新的满桶，限流永远不生效。
	lmt := newIPRateLimiter(limit, window)

	return func(c *gin.Context) {
		// 静态资源请求不执行数据库或业务逻辑，不应消耗业务接口的限流额度。
		if isStaticAssetRequest(c) {
			c.Next()
			return
		}

		httpError := tollbooth.LimitByRequest(lmt, c.Writer, c.Request)
		if httpError != nil {
			c.AbortWithStatusJSON(httpError.StatusCode, response.Response{
				Code:    httpError.StatusCode,
				Message: httpError.Message,
			})
			return
		}

		c.Next()
	}
}

// newIPRateLimiter 构造按 IP 限流的 tollbooth 限流器。
//
// 令牌桶必须显式 SetBurst：tollbooth 默认 burst=0 会导致所有请求立即被拒；
// burst 取窗口额度 limit，允许瞬时突发用满整窗配额（与「每分钟 N 次」直觉一致）。
// DefaultExpirationTTL 控制不活跃 IP 令牌桶的自动回收。
func newIPRateLimiter(limit int, window time.Duration) *limiter.Limiter {
	lmt := tollbooth.NewLimiter(float64(limit)/window.Seconds(), &limiter.ExpirableOptions{
		DefaultExpirationTTL: rateLimitBucketTTL,
	})
	lmt.SetBurst(limit).
		SetIPLookups([]string{"RemoteAddr"}).
		SetMessage("请求过于频繁").
		SetMessageContentType("application/json; charset=utf-8").
		SetStatusCode(http.StatusTooManyRequests).
		SetOnLimitReached(func(w http.ResponseWriter, r *http.Request) {
			logger.Scene("middleware").Warn("请求触发限流，已返回 429")
		})
	return lmt
}

func isStaticAssetRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/storage/")
}
