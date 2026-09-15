package userhttp

// user_middleware.go — 访客侧的会话中间件。
//
// 只有两个：attachUserSession（尽力解析，不阻断）与 requireUser（要求已登录）。
// 刻意不做「按权限点鉴权」的中间件：访客能做的事全是「操作自己的账号」，
// 归属校验写在 service 的 SQL 条件里（`WHERE id = ? AND user_id = ?`）比写在中间件里可靠 ——
// 中间件只能判断「登录没有」，判断不了「这条记录是不是他的」。

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	userenums "go_wp/internal/module/user/enums"
	userservice "go_wp/internal/module/user/service"
	"go_wp/pkg/crypto"
	"go_wp/pkg/response"
)

const (
	// ctxUserSession context 中存放当前访客会话的键。
	ctxUserSession = "user_auth_session"
)

// touchThrottleInterval 活跃时间写库的最小间隔。
//
// 不节流的话每个请求都会产生一条 UPDATE：一个页面带十几个资源请求就是十几次写。
// 活跃时间的精度要求本来就低（界面上显示「最近活跃」），60 秒完全够。
const touchThrottleInterval = 60 * time.Second

var (
	touchMu   sync.Mutex
	// key 是会话令牌哈希（= 设备列表里的 id）：一台设备一次登录一个键，
	// 与原本按台账行 id 节流等价，而会话不再落库。
	touchSeen = map[string]time.Time{}
)

// shouldTouch 判断这台设备是否到了该更新活跃时间的时候。
func shouldTouch(sessionHash string) bool {
	if strings.TrimSpace(sessionHash) == "" {
		return false
	}
	now := time.Now()
	touchMu.Lock()
	defer touchMu.Unlock()
	if last, ok := touchSeen[sessionHash]; ok && now.Sub(last) < touchThrottleInterval {
		return false
	}
	touchSeen[sessionHash] = now
	// 顺手清理过期条目：这张表按「活跃会话行」增长，
	// 不清理的话长时间运行会攒下大量再也不会出现的会话哈希。
	if len(touchSeen) > 4096 {
		for id, t := range touchSeen {
			if now.Sub(t) > 10*touchThrottleInterval {
				delete(touchSeen, id)
			}
		}
	}
	return true
}

// attachUserSession 尽力解析访客会话并挂到 context，**不阻断请求**。
//
// 未登录不是错误：注册页、登录页本身就要在未登录时可访问。
// 需要登录的入口再叠 requireUser。
func attachUserSession(svc *userservice.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := readUserToken(c)
		if token == "" {
			c.Next()
			return
		}
		sess, _ := svc.ResolveSession(c.Request.Context(), token)
		if sess != nil {
			c.Set(ctxUserSession, sess)
			// 令牌也要挂到 context：登出与「退出其它设备」都要用它，
			// 而从 cookie 再读一次会让「cookie 已被清掉」这类边界出现分歧。
			c.Set(userSessionTokenKey, token)
			if shouldTouch(crypto.Sha256(token)) {
				svc.TouchSession(c.Request.Context(), sess)
			}
		}
		c.Next()
	}
}

// requireUser 要求已登录：页面请求 302 到登录页（带 next 回跳），其余返回 401。
//
// next 必须是本站路径（见 safeNext）：不加限制的话
// `/user/login?next=https://evil.example` 看起来就是本站的登录链接，
// 登录成功后把用户送去别处 —— 典型的开放重定向。
func requireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentSession(c) != nil {
			c.Next()
			return
		}
		if wantsJSON(c) {
			// JSON 响应统一走 pkg/response（模块规范：不自己拼 {code,message} 结构）。
			response.ErrorWithMessage(c, http.StatusUnauthorized, userenums.ErrNotLoggedIn)
			c.Abort()
			return
		}
		next := c.Request.URL.RequestURI()
		c.Redirect(http.StatusFound, "/user/login?next="+url.QueryEscape(next))
		c.Abort()
	}
}

// wantsJSON 判断调用方期望 JSON 而不是页面（HTMX / fetch 请求）。
func wantsJSON(c *gin.Context) bool {
	if c.GetHeader("HX-Request") != "" {
		return false // HTMX 要的是 HTML 片段
	}
	accept := c.GetHeader("Accept")
	return accept != "" && !strings.Contains(accept, "text/html")
}

// currentSession 取当前请求的访客会话；未登录返回 nil。
func currentSession(c *gin.Context) *userservice.UserAuthSession {
	v, ok := c.Get(ctxUserSession)
	if !ok {
		return nil
	}
	s, ok := v.(*userservice.UserAuthSession)
	if !ok {
		return nil
	}
	return s
}

// currentToken 取当前请求携带的会话令牌。
func currentToken(c *gin.Context) string {
	if v, ok := c.Get(userSessionTokenKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return readUserToken(c)
}

// safeNext 校验回跳地址：只允许本站绝对路径。
//
// 允许的形态：以单个 "/" 开头且不是 "//"（"//evil.com" 在浏览器里是协议相对 URL，
// 会跳到外站）。其余一律回落到 fallback。
func safeNext(next, fallback string) string {
	next = strings.TrimSpace(next)
	if next == "" || next[0] != '/' || (len(next) > 1 && next[1] == '/') {
		return fallback
	}
	// 反斜杠：历史浏览器把 `/\evil.com` 也当协议相对 URL 处理，这里一并挡掉，
	// 代价是拒绝一个几乎不存在的合法路径。
	if strings.Contains(next, "\\") {
		return fallback
	}
	return next
}
