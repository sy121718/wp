package userhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	mw "go_wp/pkg/middleware"
	"go_wp/pkg/response"
)

func setupSiteUserRoutes(router *gin.Engine, h *Handle) {
	api := mw.NewRouteGroup(router.Group("/api/site/user", attachUserSession(h.svc), VisitorCSRFMiddleware()), mw.RouteProtected)
	api.GET("/session", "", h.SiteSession)
	limit := builtin.RequestRateLimitMiddleware(userSensitiveRateLimit, userSensitiveRateWindow)
	api.POST("/login", "", limit, h.SiteLogin)
	api.POST("/register", "", limit, h.SiteRegister)
	api.POST("/forgot", "", limit, h.SiteForgot)
	api.POST("/activate", "", limit, h.SiteActivate)
	api.POST("/resend", "", limit, h.SiteResendActivation)
	api.POST("/reset", "", limit, h.SiteReset)
	api.POST("/logout", "", h.SiteLogout)
	own := api.Group("", requireSiteUser())
	own.GET("/account", "", h.SiteAccount)
	own.GET("/sessions", "", h.SiteDevices)
	own.POST("/account/profile", "", h.SiteUpdateProfile)
	own.POST("/account/preference", "", h.SiteUpdatePreference)
	own.POST("/account/password", "", limit, h.SiteChangePassword)
	own.POST("/sessions/revoke", "", h.SiteRevokeDevice)
	own.POST("/sessions/revoke-others", "", h.SiteRevokeOtherDevices)
}

func requireSiteUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if siteUserID(c) == 0 {
			response.ErrorStatusAuto(c, "user", userenums.Err(userenums.ErrNotLoggedIn))
			c.Abort()
			return
		}
		c.Next()
	}
}

func siteUserID(c *gin.Context) uint64 {
	if session := currentSession(c); session != nil {
		return session.UserID
	}
	return 0
}

func (h *Handle) saveSiteLogin(c *gin.Context, token string, remember bool) error {
	if err := writeUserToken(c, token, remember); err != nil {
		_ = h.svc.Logout(c.Request.Context(), token)
		return err
	}
	csrf, err := builtin.RotateCSRFTokenWith(c, userCSRFStore{})
	if err != nil {
		_ = h.svc.Logout(c.Request.Context(), token)
		_ = clearUserSession(c)
		return err
	}
	c.Header("X-CSRF-Token", csrf)
	return nil
}

func (h *Handle) changeSitePasswordSession(c *gin.Context, req *userdto.ChangePasswordReq) error {
	if err := h.svc.ChangePassword(c.Request.Context(), req); err != nil {
		return err
	}
	return clearUserSession(c)
}
