// Package router 验证码公共模块路由注册。
package router

import (
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/common/captcha/handle"

	"github.com/gin-gonic/gin"
)

// 验证码获取接口按 IP 限流参数：防止匿名脚本批量拉取验证码图片，
// 既消耗 CPU（PNG 生成）又撑大内存 Store。正常用户 60s 内刷新
// 10 次已足够（登录失败重试也会拉新图）。
const (
	captchaRateLimit  = 10
	captchaRateWindow = time.Minute
)

// SetupCaptchaRoutes 注册验证码相关路由。
//
// 该接口匿名可达（登录前置依赖），因此无条件挂载按 IP 限流中间件，
// 不依赖全局 server.rate_limit_enabled 开关——保证验证码在任何部署下
// 都不会被无限刷爆。
func SetupCaptchaRoutes(rg *gin.RouterGroup) {
	if rg == nil {
		return
	}

	rg.GET("/captcha",
		builtin.RequestRateLimitMiddleware(captchaRateLimit, captchaRateWindow),
		handle.CaptchaHandle)
}
