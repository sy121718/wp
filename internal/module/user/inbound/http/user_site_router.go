package userhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/response"
)

func setupSiteUserRoutes(router *gin.Engine, h *Handle) {
	api := permission.NewRouteGroup(router.Group("/api/site/user", attachUserSession(h.svc), VisitorCSRFMiddleware()))
	api.GET("/session", permission.Exempt, h.SiteSession)
	limit := builtin.RequestRateLimitMiddleware(userSensitiveRateLimit, userSensitiveRateWindow)
	api.POST("/login", permission.Exempt, limit, h.SiteLogin)
	api.POST("/register", permission.Exempt, limit, h.SiteRegister)
	api.POST("/forgot", permission.Exempt, limit, h.SiteForgot)
	api.POST("/activate", permission.Exempt, limit, h.SiteActivate)
	api.POST("/resend", permission.Exempt, limit, h.SiteResendActivation)
	api.POST("/reset", permission.Exempt, limit, h.SiteReset)
	api.POST("/logout", permission.Exempt, h.SiteLogout)
	own := api.Group("", requireSiteUser())
	own.GET("/account", permission.Exempt, h.SiteAccount)
	own.GET("/sessions", permission.Exempt, h.SiteDevices)
	own.POST("/account/profile", permission.Exempt, h.SiteUpdateProfile)
	own.POST("/account/preference", permission.Exempt, h.SiteUpdatePreference)
	own.POST("/account/password", permission.Exempt, limit, h.SiteChangePassword)
	own.POST("/sessions/revoke", permission.Exempt, h.SiteRevokeDevice)
	own.POST("/sessions/revoke-others", permission.Exempt, h.SiteRevokeOtherDevices)
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
