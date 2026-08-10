// Package router 验证码公共模块路由注册。
package router

import (
	"go_wp/internal/module/common/captcha/handle"

	"github.com/gin-gonic/gin"
)

// SetupCaptchaRoutes 注册验证码相关路由。
func SetupCaptchaRoutes(rg *gin.RouterGroup) {
	if rg == nil {
		return
	}

	rg.GET("/captcha", handle.CaptchaHandle)
}
