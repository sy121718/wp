package builtin

import (
	"go_wp/pkg/auth"
	"go_wp/pkg/enums"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func SessionAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		cs, err := auth.GetCookieSession(c)
		if err != nil || cs == nil || cs.SessionID == "" {
			if err != nil {
				logger.Scene("middleware").With("reason", "会话读取失败").With("err", err).Warn("认证失败")
			}
			respondAuthFailure(c, enums.ErrUnauthorized)
			c.Abort()
			return
		}

		idx, err := auth.GetSessionIndex(c.Request.Context(), cs.SessionID)
		if err != nil {
			respondAuthFailure(c, enums.ErrAuthUnavailable)
			c.Abort()
			return
		}
		if idx == nil || idx.UserID == 0 {
			logger.Scene("middleware").With("reason", "会话句柄无效或已失效").Warn("认证失败")
			respondAuthFailure(c, enums.ErrSessionExpired)
			c.Abort()
			return
		}

		blocked, err := auth.IsBlocked(c.Request.Context(), idx.UserID, idx.IssuedAt)
		if err != nil {
			respondAuthFailure(c, enums.ErrAuthUnavailable)
			c.Abort()
			return
		}
		if blocked {
			logger.Scene("middleware").With("reason", "账号被封禁下线").Warn("认证失败")
			respondAuthFailure(c, enums.ErrForcedLogout)
			c.Abort()
			return
		}

		session, err := auth.GetUserSession(c.Request.Context(), idx.UserID)
		if err != nil {
			respondAuthFailure(c, enums.ErrAuthUnavailable)
			c.Abort()
			return
		}
		if session == nil || session.SessionID != cs.SessionID {
			logger.Scene("middleware").With("reason", "登录会话已失效").Warn("认证失败")
			respondAuthFailure(c, enums.ErrSessionExpired)
			c.Abort()
			return
		}

		c.Set("user_id", int64(idx.UserID))
		c.Set("username", session.Username)
		c.Next()

		if c.GetBool(auth.ContextSessionRevokedKey) {
			return
		}

		if err := auth.RefreshOnline(c.Request.Context(), idx.UserID, 0); err != nil {
			logger.Scene("middleware").With("err", err).Warn("刷新在线心跳失败")
		}
	}
}

func respondAuthFailure(c *gin.Context, key string) {
	c.Header("Cache-Control", "no-store")
	response.ErrorStatusAuto(c, "middleware", enums.AuthError(key))
}
