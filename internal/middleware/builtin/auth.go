package builtin

import (
	"log"
	"strings"
	"time"

	"go_wp/pkg/auth"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// JWTAuthMiddleware JWT 认证与自动续期中间件。
//
// 认证校验：
//  1. 从 Authorization 头中提取 Bearer token
//  2. 调用 auth.ParseToken() 解析 JWT，验证签名和过期时间
//  3. 解析成功后，将 user_id 和 username 写入 gin.Context
//
// 自动续期：
//
//	请求处理完成后，若 token 剩余有效期不足 1 小时，
//	自动生成新 token 并通过 X-New-Token 响应头返回前端。
//	前端 Axios 拦截器检测到此头后自动更新本地存储，实现无感续期。
//
// 失败场景：
//   - 未携带 Authorization 头 → 返回 401 "未登录或登录已过期"
//   - Authorization 格式错误（非 Bearer） → 返回 401
//   - token 无效或已过期 → 返回 401
//
// 适用位置：需要登录认证的路由组或单路由。
func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		//提取请求头中的 Authorization 字段
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.ErrorWithMessage(c, 401, "未登录或登录已过期")
			c.Abort()
			return
		}

		//分割 Authorization 字段，格式为 Bearer <token>
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.ErrorWithMessage(c, 401, "未登录或登录已过期")
			c.Abort()
			return
		}
		claims, err := auth.ParseToken(parts[1])
		//解析 token 失败
		if err != nil {
			response.ErrorWithMessage(c, 401, "环境异常,请重新登录")
			c.Abort()
			return
		}
		//blocked 封禁标记
		// 检查 Redis 封禁标记
		blocked, err := auth.IsBlocked(c.Request.Context(), uint64(claims.UserID), claims.IssuedAt.Unix())
		if err != nil {
			response.ErrorWithMessage(c, 503, "认证状态暂时不可用")
			c.Abort()
			return
		}
		if blocked {
			response.ErrorWithMessage(c, 401, "账号已被强制下线")
			c.Abort()
			return
		}

		session, err := auth.GetUserSession(c.Request.Context(), uint64(claims.UserID))
		if err != nil {
			response.ErrorWithMessage(c, 503, "认证状态暂时不可用")
			c.Abort()
			return
		}
		if session == nil || claims.SessionID == "" || session.SessionID != claims.SessionID {
			response.ErrorWithMessage(c, 401, "登录会话已失效，请重新登录")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()

		if c.GetBool(auth.ContextSessionRevokedKey) {
			return
		}

		// 自动续期：token 剩余不足 10 分钟，生成新 token 通过响应头返回
		if claims.ExpiresAt != nil && time.Until(claims.ExpiresAt.Time) <= 10*time.Minute {
			newToken, _, err := auth.GenerateSessionToken(claims.UserID, claims.Username, claims.RememberMe, claims.SessionID)
			if err != nil {
				log.Printf("JWT 自动续期失败: %v", err)
				return
			}
			c.Header("X-New-Token", newToken)
		}

		// 刷新在线心跳
		if err := auth.RefreshOnline(c.Request.Context(), uint64(claims.UserID), 0); err != nil {
			log.Printf("刷新在线心跳失败: %v", err)
		}
	}
}
