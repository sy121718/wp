package router

import (
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/common/captcha/handle"
	mw "go_wp/pkg/middleware"

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

	mw.NewRouteGroup(rg, mw.RouteProtected).GET("/captcha", "",
		builtin.RequestRateLimitMiddleware(captchaRateLimit, captchaRateWindow),
		handle.CaptchaHandle)
}
