package userhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/response"
)

// setupSiteUserRoutes 直接挂载访客 API，独立于后台 Session/CSRF/Casbin。
func setupSiteUserRoutes(router *gin.Engine, h *Handle) {
	api := router.Group("/api/site/user", attachUserSession(h.svc), VisitorCSRFMiddleware())
	api.GET("/session", h.SiteSession)
	limit := builtin.RequestRateLimitMiddleware(userSensitiveRateLimit, userSensitiveRateWindow)
	api.POST("/login", limit, h.SiteLogin)
	api.POST("/register", limit, h.SiteRegister)
	api.POST("/forgot", limit, h.SiteForgot)
	api.POST("/reset", limit, h.SiteReset)
	api.POST("/logout", h.SiteLogout)
	own := api.Group("", requireSiteUser())
	own.GET("/account", h.SiteAccount)
	own.GET("/sessions", h.SiteDevices)
	own.POST("/account/profile", h.SiteUpdateProfile)
	own.POST("/account/preference", h.SiteUpdatePreference)
	own.POST("/account/password", limit, h.SiteChangePassword)
	own.POST("/sessions/revoke", h.SiteRevokeDevice)
	own.POST("/sessions/revoke-others", h.SiteRevokeOtherDevices)
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

// saveSiteLogin 令牌只写访客cookie；CSRF轮换成功后才完成登录响应。
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

// changeSitePasswordSession 在服务完成改密和会话撤销后清除当前访客cookie。
func (h *Handle) changeSitePasswordSession(c *gin.Context, req *userdto.ChangePasswordReq) error {
	if err := h.svc.ChangePassword(c.Request.Context(), req); err != nil {
		return err
	}
	return clearUserSession(c)
}
