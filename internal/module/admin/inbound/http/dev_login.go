// dev_login.go — 开发阶段一键登录（issue：本地开发 / 自动化验证的登录便利）。
//
// ## 为什么要它
//
// 后台每个页面都要登录，而登录要过验证码 —— 人肉点一次还行，用浏览器做端到端验证时
// 每次会话重建都要重来一遍。于是有了这个入口：
//
//	http://127.0.0.1:8080/admin/dev-login?to=/admin/mail/automation/canvas?id=1
//
// ## 四道锁（这是后门形状的东西，必须锁死）
//
//  1. **release 模式根本不注册这个路由**（见 routers/routes.go），不是「注册了再判断」。
//     不存在的路由无法被利用，比运行时 if 更可靠。
//  2. **只认环回地址**：用 c.Request.RemoteAddr 而不是 c.ClientIP() —— 后者会被
//     X-Forwarded-For 影响，代理配置不当就能从外部伪造出一个「本机请求」。
//  3. **只允许登录超管**（is_admin=1）：不会拿它去登普通管理员，避免越权面。
//  4. **走与正常登录完全同一条会话路径**（auth.NewSessionID + SaveUserSession +
//     RefreshOnline + RotateCSRFToken）：不做「绕过校验直接塞 cookie」的旁路，
//     否则开发环境的行为与生产不一致，验证也失去意义。每次使用都写日志。
package adminhttp

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/builtin"
	adminservice "go_wp/internal/module/admin/service"
	"go_wp/pkg/auth"
	"go_wp/pkg/logger"
)

// DevLogin 开发阶段免密登录：建立超管会话后 302 到目标页面。
//
// 只应在 debug 模式下注册。参数：
//   - to     登录后跳转的路径（以 / 开头；缺省 /admin）
//   - user   可选，指定超管用户名（缺省取第一个超管）
func (h *Handle) DevLogin(c *gin.Context) {
	// 锁 2：只认环回。RemoteAddr 形如 127.0.0.1:52331 / [::1]:52331。
	if !isLoopbackRemote(c.Request.RemoteAddr) {
		logger.Scene("admin").With("remote", c.Request.RemoteAddr).Warn("拒绝非本机的一键登录请求")
		c.String(http.StatusForbidden, "开发登录仅限本机访问")
		return
	}

	res, err := h.admin.DevLogin(c.Request.Context(), strings.TrimSpace(c.Query("user")))
	if err != nil {
		logger.Scene("admin").Error(err, "开发登录失败")
		c.String(http.StatusInternalServerError, "开发登录失败，详情见服务端日志")
		return
	}

	// 锁 4：与正常登录同一条会话路径（cookie + Redis + CSRF 轮换）。
	if err := auth.SaveCookieSession(c, &auth.CookieSession{
		UserID:    res.UserID,
		Username:  res.Username,
		SessionID: res.SessionID,
		IssuedAt:  res.IssuedAt,
	}, false); err != nil {
		logger.Scene("admin").Error(err, "开发登录写会话失败")
		c.String(http.StatusInternalServerError, "写会话失败，详情见服务端日志")
		return
	}
	if _, err := builtin.RotateCSRFToken(c); err != nil {
		logger.Scene("admin").Error(err, "开发登录轮换 CSRF token 失败")
		c.String(http.StatusInternalServerError, "轮换 CSRF token 失败，详情见服务端日志")
		return
	}

	to := safeRedirect(c.Query("to"))
	logger.Scene("admin").With("username", res.Username).With("to", to).
		Info("开发阶段一键登录（仅 debug 模式可用）")
	c.Redirect(http.StatusFound, to)
}

// DevLoginHandler 供装配层在 debug 模式下挂载（见 internal/routers/routes.go）。
//
// 独立工厂而不是复用 SetupAdminRoutes 里的 handle：那个 handle 服务的是 /api/admin/* 组，
// 而一键登录是**页面**路由（浏览器直接访问 /admin/dev-login）。
func DevLoginHandler(db *gorm.DB) gin.HandlerFunc {
	return NewHandle(adminservice.NewService(db)).DevLogin
}

// isLoopbackRemote 判断远端地址是否为本机（用于把开发登录限制在本机）。
func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// safeRedirect 只接受站内绝对路径，其余一律回 /admin。
//
// 开放重定向在「一键登录」这种带凭据的入口上尤其危险：
// 一个 https://evil.com 的 to 参数能让人以为自己在登录本站。
func safeRedirect(to string) string {
	to = strings.TrimSpace(to)
	if to == "" || !strings.HasPrefix(to, "/") {
		return "/admin"
	}
	// "//evil.com" 是协议相对 URL，浏览器会当外站处理，必须挡住。
	if strings.HasPrefix(to, "//") || strings.HasPrefix(to, "/\\") {
		return "/admin"
	}
	return to
}
