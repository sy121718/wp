package router

import (
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/captcha"
	mw "go_wp/pkg/middleware"

	"github.com/gin-gonic/gin"
)

const (
	captchaRateLimit  = 10
	captchaRateWindow = time.Minute
)

func SetupCaptchaRoutes(reg *mw.Registry, rg *gin.RouterGroup) {
	if rg == nil {
		return
	}

	mw.NewRouteGroupIn(reg, rg, mw.RouteExempt).GET("/captcha", "",
		builtin.RequestRateLimitMiddleware(captchaRateLimit, captchaRateWindow),
		captcha.HTTPServe)
}
