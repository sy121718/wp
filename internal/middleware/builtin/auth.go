package builtin

import (
	"net/http"
	"strings"

	"go_wp/pkg/auth"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// loginPagePath 登录页路径。
//
// 说明：当前项目尚无独立的页面登录路由（dashboard 未注册 /admin/login 页面，
// internal/templates 下亦无 login 模板），此处指向 /api/admin/login 对应的
// 页面路径 /admin/login。页面路由落地后本常量无需再调整。
const loginPagePath = "/admin/login"

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
// 失败场景（未登录态统一按请求类型区分响应）：
//   - 无 cookie session / session 无效 / 会话已失效 / 账号被封禁：
//     HTMX 请求（HX-Request: true）或 Accept 含 text/html 的 GET → 302 重定向登录页；
//     其余（JSON API / XHR）→ 401 JSON（response.ErrorWithMessage）
//   - Redis 不可用 → 返回 503 "认证状态暂时不可用"（系统错误，不区分请求类型）
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
			respondAuthRequired(c, "未登录或登录已过期")
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
			respondAuthRequired(c, "账号已被强制下线")
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
			respondAuthRequired(c, "登录会话已失效，请重新登录")
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

// respondAuthRequired 未登录态的统一响应：按请求类型区分。
//
// - HTMX 请求（HX-Request: true）：302 重定向到登录页（HTMX 自动跟随跳转）
// - Accept 含 text/html 的 GET（浏览器直接打开页面）：302 重定向到登录页
// - 其余（JSON API / 非浏览器 XHR）：401 JSON，保持 response.ErrorWithMessage 风格
func respondAuthRequired(c *gin.Context, message string) {
	if wantsLoginPage(c) {
		c.Redirect(http.StatusFound, loginPagePath)
		return
	}
	response.ErrorWithMessage(c, http.StatusUnauthorized, message)
}

// wantsLoginPage 判断当前请求是否期望页面（应重定向登录页而非返回 JSON）。
func wantsLoginPage(c *gin.Context) bool {
	// HTMX 请求：任意 method 都按页面交互处理（POST 表单提交后登录过期同样跳转）。
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("HX-Request")), "true") {
		return true
	}
	// 浏览器页面导航：GET 且 Accept 声明可接收 HTML。
	return c.Request.Method == http.MethodGet &&
		strings.Contains(c.GetHeader("Accept"), "text/html")
}
