package mailhttp

// mail_public_router.go — 邮件追踪的**公开面**注册落点。
//
// 与 mail_router.go 的差别要写清楚：这里的三条路由挂在引擎根级、**没有会话也没有 Casbin** ——
// 调用方是收件人点开邮件里的链接，不是后台用户。鉴权靠 token 本身（不可猜、可失效），
// 与后台的 Session + CSRF + Casbin 三层链不是一回事，不要往这里挂后台中间件。
//
// handler 在 mail_page.go；路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/module/mail/contract"
)

// SetupTrackingRoutes 挂载追踪路由（公开）。
func SetupTrackingRoutes(router *gin.Engine, svc mailcontract.TrackingService) {
	if router == nil || svc == nil {
		return
	}
	h := NewTrackingHandle(svc)
	router.GET("/_t/o/:token", h.Open)
	router.GET("/_t/c/:token", h.Click)
	router.GET("/_t/u/:token", h.Unsubscribe)
}
