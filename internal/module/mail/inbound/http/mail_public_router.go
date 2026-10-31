package mailhttp

import (
	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
)

func SetupTrackingRoutes(router *gin.Engine, svc mailcontract.TrackingService) {
	if router == nil || svc == nil {
		return
	}
	h := NewTrackingHandle(svc)
	router.GET("/_t/o/:token", h.Open)
	router.GET("/_t/c/:token", h.Click)
	router.GET("/_t/u/:token", h.Unsubscribe)
}
