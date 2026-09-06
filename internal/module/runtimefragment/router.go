// Package runtimefragment — 片段端点路由注册（0-D，docs/04 §1.2）。
// 公开路由（不挂 SessionAuth）：loginPanel 等 anonymous capability 未登录
// 访客可见；session 策略由 endpoint 内部按 capability 自行校验。
package runtimefragment

import (
	"github.com/gin-gonic/gin"
)

// SetupFragmentRoutes 注册片段端点路由（挂载到引擎根路径）。
func SetupFragmentRoutes(router *gin.Engine) {
	if router == nil {
		return
	}
	// GET /_fragments/{type}：白名单校验 + 认证策略 + 处理器。
	router.GET("/_fragments/:type", FragmentEndpoint)
	// POST 写操作能力（购物车等）后续按 capability 注册（带 CSRF）。
}
