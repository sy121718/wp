package router

import (
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/common/captcha/handle"
	"go_wp/internal/permission"

	"github.com/gin-gonic/gin"
)

const (
	captchaRateLimit  = 10
	captchaRateWindow = time.Minute
)

func SetupCaptchaRoutes(rg *gin.RouterGroup) {
	if rg == nil {
		return
	}

	permission.NewRouteGroup(rg).GET("/captcha", permission.Exempt,
		builtin.RequestRateLimitMiddleware(captchaRateLimit, captchaRateWindow),
		handle.CaptchaHandle)
}
