// Package runtimefragment — 片段端点路由注册（0-D，docs/04 §1.2）。
// 公开路由（不挂 SessionAuth）：loginPanel 等 anonymous capability 未登录
// 访客可见；session 策略由 endpoint 内部按 capability 自行校验。
package runtimefragment

import (
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
)

const (
	fragmentRateLimit  = 90
	fragmentRateWindow = time.Minute
)

// SetupFragmentRoutes 注册片段端点路由（挂载到引擎根路径）。
func SetupFragmentRoutes(router *gin.Engine) {
	if router == nil {
		return
	}
	handlers := []gin.HandlerFunc{
		builtin.RequestRateLimitMiddleware(fragmentRateLimit, fragmentRateWindow),
	}
	if deps.VisitorIdentityMiddleware != nil {
		handlers = append(handlers, deps.VisitorIdentityMiddleware)
	}
	handlers = append(handlers, FragmentEndpoint)
	// GET /_fragments/{type}：白名单校验 + 认证策略 + 处理器。
	router.GET("/_fragments/:type", handlers...)
	// POST /_fragments/{type}（issue #20）：结构化入参（并行数组）用表单传，
	// 能力声明 Method=POST 才可达。仍然是逐 capability 白名单，不是任意 endpoint。
	// 注意：需要写状态（购物车 / 下单）的 POST 能力必须声明 session 策略并带 CSRF，
	// 不能沿用 anonymous（见 endpoint.go 顶部说明）。
	router.POST("/_fragments/:type", handlers...)
}
