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
// 说明：页面登录路由由 admin 模块注册（SetupAdminShellPages → /admin/login，
// 模板 internal/templates/admin/login.html），与 /api/admin/login 是同一路径的
// 页面侧；本常量与它保持同值。
const loginPagePath = "/admin/login"

// SessionAuthMiddleware Session + Cookie 认证中间件。
//
// 认证校验（P1 句柄化后的顺序）：
//  1. 从 cookie 取出会话句柄（auth.GetCookieSession）—— cookie 里没有身份，只有一个随机串
//  2. 句柄 → 身份：查服务端索引（auth.GetSessionIndex），查不到按未登录处理
//  3. 检查 Redis 封禁标记（auth.IsBlocked），基准时间取自服务端索引
//  4. 校验 Redis 用户会话（auth.GetUserSession）与句柄是否一致（单会话语义落在这里）
//  5. 通过后把 user_id（int64）和 username 写入 gin.Context，保持旧 context key 不变，
//     避免下游 casbin / datarule 改动；username 取自服务端会话，不取自 cookie
//
// 第 2 步是句柄化的关键：身份只可能由服务端给出，客户端无从声明自己是谁 ——
// 「先信任声明、再回头核对」这个形状（连同漏掉核对的风险）从根上消失了。
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
		// 1) 从 cookie 取出会话句柄。cookie 里**没有身份** —— 只有一个不透明随机串，
		//    所以「我是谁」不可能由客户端声明（P1 句柄化）。
		cs, err := auth.GetCookieSession(c)
		if err != nil || cs == nil || cs.SessionID == "" {
			if err != nil {
				logger.Scene("middleware").With("reason", "会话读取失败").With("err", err).Warn("认证失败")
			}
			respondAuthRequired(c, "未登录或登录已过期")
			c.Abort()
			return
		}

		// 2) 句柄 → 身份：身份只可能来自服务端映射（Redis）。句柄无效 / 已过期 /
		//    已登出一律按未登录处理、不区分原因 —— 区分会让「Redis 挂了」与「没登录」
		//    走成两条路径，而它们对用户应当完全一样（去登录）。
		idx, err := auth.GetSessionIndex(c.Request.Context(), cs.SessionID)
		if err != nil {
			response.ErrorWithMessage(c, 503, "认证状态暂时不可用")
			c.Abort()
			return
		}
		if idx == nil || idx.UserID == 0 {
			logger.Scene("middleware").With("reason", "会话句柄无效或已失效").Warn("认证失败")
			respondAuthRequired(c, "登录会话已失效，请重新登录")
			c.Abort()
			return
		}

		// 3) 检查 Redis 封禁标记。
		//    基准时间取自服务端索引而不是 cookie —— 这正是句柄化的收益之一：
		//    封禁判断的依据不再是一个客户端可以声称的值。
		blocked, err := auth.IsBlocked(c.Request.Context(), idx.UserID, idx.IssuedAt)
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

		// 4) 校验 Redis 用户会话与句柄是否仍然一致。
		//    「单会话」语义就落在这里：登录会覆盖 user:session:{userID}，
		//    于是同一账号此前建立的句柄全部对不上 → 后登录顶掉先登录。
		session, err := auth.GetUserSession(c.Request.Context(), idx.UserID)
		if err != nil {
			response.ErrorWithMessage(c, 503, "认证状态暂时不可用")
			c.Abort()
			return
		}
		if session == nil || session.SessionID != cs.SessionID {
			logger.Scene("middleware").With("reason", "登录会话已失效").Warn("认证失败")
			respondAuthRequired(c, "登录会话已失效，请重新登录")
			c.Abort()
			return
		}

		// 5) 写入 context（key 与旧 JWT 中间件保持一致）
		c.Set("user_id", int64(idx.UserID))
		c.Set("username", session.Username)
		c.Next()

		if c.GetBool(auth.ContextSessionRevokedKey) {
			return
		}

		// 5) 刷新在线心跳
		if err := auth.RefreshOnline(c.Request.Context(), idx.UserID, 0); err != nil {
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
