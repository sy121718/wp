package builtin

import (
	"go_wp/pkg/auth"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// SessionAuthMiddleware Session + Cookie 认证中间件。
//
// 认证校验：
//  1. 从 cookie session 读取认证会话（auth.GetCookieSession），替代旧的 Authorization 头 + JWT
//  2. 检查 Redis 封禁标记（auth.IsBlocked）
//  3. 校验 Redis 用户会话（auth.GetUserSession）与 cookie 中的 SessionID 是否一致
//  4. 通过后把 user_id（int64）和 username 写入 gin.Context，保持旧 context key 不变，
//     避免下游 casbin / datarule 改动
//
// 请求处理完成后刷新在线心跳（auth.RefreshOnline）。
// 无需 JWT 自动续期：cookie 由浏览器自动携带，过期由 Cookie MaxAge 控制。
//
// 失败场景：
//   - 无 cookie session / session 无效 → 返回 401 "未登录或登录已过期"
//   - Redis 不可用 → 返回 503 "认证状态暂时不可用"
//   - 账号被封禁 → 返回 401 "账号已被强制下线"
//
// 适用位置：需要登录认证的路由组或单路由。
func SessionAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1) 从 cookie session 读取用户（替代 Authorization 头 + JWT）
		cs, err := auth.GetCookieSession(c)
		if err != nil || cs == nil {
			if err != nil {
				logger.Scene("middleware").With("reason", "会话读取失败").With("err", err).Warn("认证失败")
			}
			response.ErrorWithMessage(c, 401, "未登录或登录已过期")
			c.Abort()
			return
		}

		// 2) 检查 Redis 封禁标记
		blocked, err := auth.IsBlocked(c.Request.Context(), cs.UserID, cs.IssuedAt)
		if err != nil {
			response.ErrorWithMessage(c, 503, "认证状态暂时不可用")
			c.Abort()
			return
		}
		if blocked {
			logger.Scene("middleware").With("reason", "账号被封禁下线").Warn("认证失败")
			response.ErrorWithMessage(c, 401, "账号已被强制下线")
			c.Abort()
			return
		}

		// 3) 校验 Redis 用户会话与会话 ID
		session, err := auth.GetUserSession(c.Request.Context(), cs.UserID)
		if err != nil {
			response.ErrorWithMessage(c, 503, "认证状态暂时不可用")
			c.Abort()
			return
		}
		if session == nil || cs.SessionID == "" || session.SessionID != cs.SessionID {
			logger.Scene("middleware").With("reason", "登录会话已失效").Warn("认证失败")
			response.ErrorWithMessage(c, 401, "登录会话已失效，请重新登录")
			c.Abort()
			return
		}

		// 4) 写入 context（key 与旧 JWT 中间件保持一致）
		c.Set("user_id", int64(cs.UserID))
		c.Set("username", cs.Username)
		c.Next()

		if c.GetBool(auth.ContextSessionRevokedKey) {
			return
		}

		// 5) 刷新在线心跳
		if err := auth.RefreshOnline(c.Request.Context(), cs.UserID, 0); err != nil {
			logger.Scene("middleware").With("err", err).Warn("刷新在线心跳失败")
		}
	}
}
