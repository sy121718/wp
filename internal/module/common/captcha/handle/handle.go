package handle

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/captcha"
)

// CaptchaHandle 保留既有调用名；通用实现归 pkg/captcha。
func CaptchaHandle(c *gin.Context) { captcha.HTTPServe(c) }
