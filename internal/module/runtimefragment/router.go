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

// visitorIdentityMiddleware 访客身份解析中间件（装配期注入，可缺）。
//
// 可缺是刻意的：片段层与 user 模块互相独立，单元测试与最小装配下
// 没有它也能跑（只是所有能力看到的 UserID 都是空）。漏接的表现是
// 「访客订单永远显示未登录」，属于功能缺失而不是安全缺口。
var visitorIdentityMiddleware func(c *gin.Context)

// SetVisitorIdentityMiddleware 注入访客身份中间件（装配期调用）。
func SetVisitorIdentityMiddleware(mw func(c *gin.Context)) {
	visitorIdentityMiddleware = mw
}

// SetupFragmentRoutes 注册片段端点路由（挂载到引擎根路径）。
func SetupFragmentRoutes(router *gin.Engine) {
	if router == nil {
		return
	}
	handlers := []gin.HandlerFunc{
		builtin.RequestRateLimitMiddleware(fragmentRateLimit, fragmentRateWindow),
	}
	if visitorIdentityMiddleware != nil {
		handlers = append(handlers, visitorIdentityMiddleware)
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
