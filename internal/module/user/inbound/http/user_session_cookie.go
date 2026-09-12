// Package userhttp 用户模块（访客账号）的 HTTP 接入层：注册 / 验证 / 登录 / 账号中心。
//
// # 为什么这些路由不在 /api 下
//
// /api 那组挂了 SessionAuthMiddleware + CSRFMiddleware + CasbinMiddleware 三件套，
// 它们是**管理后台**的认证体系：Casbin 的 subject 是 sys_admin.id，权限点是后台菜单的权限点。
// 访客账号没有权限点、也不该进 Casbin 的策略表（那会让「登录」这件事变成一次授权决策）。
//
// 所以访客侧自带一套**更窄**的链路：自己的 cookie 会话 + 自己的 CSRF token，
// 没有 Casbin。少一层不是省事，而是把「谁有权做什么」收敛到业务代码里 ——
// 访客能做的事只有「操作自己的账号」，这个判断不需要策略引擎。
package userhttp

// user_session_cookie.go — 访客 cookie 会话与 CSRF token 存储。
//
// cookie 里只放会话令牌本身，业务字段一律不入 cookie：
// cookie 是客户端可见的（虽然签名防篡改），放进去的东西改起来麻烦、还会随每个请求来回传。

import (
	"errors"
	"strings"
	"sync"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	gsessions "github.com/gorilla/sessions"

	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/auth"
)

const (
	// userSessionCookieName 访客会话 cookie 名。
	//
	// **必须与后台的 gowp_session 不同**：同一个 cookie 只能存一份会话，
	// 共用会让「后台开着 + 前台登录一次」直接把管理员顶出后台。
	userSessionCookieName = "gowp_user_session"

	// userSessionTokenKey cookie 会话里存令牌的键。
	userSessionTokenKey = "user_token"
	// userCSRFKey CSRF token 在访客会话里的键。
	userCSRFKey = "csrf_token"

	// 有效期与 Redis 会话、user_sessions 台账口径一致（见 service.SessionTTLFor）。
	userSessionMaxAge         = 24 * 60 * 60
	userSessionRememberMaxAge = 7 * 24 * 60 * 60
)

var (
	userStoreMu     sync.RWMutex
	userCookieStore sessions.Store
)

// setupUserCookieStore 建访客的 cookie store（由 SetupUserRoutes 在装配期调用）。
//
// 用 pkg/auth 的 NamedCookieStore 而不是自己 new 一个 store：密钥与后台同源
// （同一个 auth.session_secret），差异只在 cookie 名。各建一份密钥会让轮换密钥时漏掉一个。
func setupUserCookieStore() error {
	store, err := auth.NamedCookieStore(userSessionCookieName)
	if err != nil {
		return err
	}
	userStoreMu.Lock()
	userCookieStore = store
	userStoreMu.Unlock()
	return nil
}

// userSession 取当前请求的访客 cookie 会话（gorilla 按请求缓存，同一请求多次调用返回同一对象）。
func userSession(c *gin.Context) (*gsessions.Session, error) {
	userStoreMu.RLock()
	store := userCookieStore
	userStoreMu.RUnlock()
	if store == nil {
		return nil, errors.New("访客会话存储未初始化")
	}
	return store.Get(c.Request, userSessionCookieName)
}

// readUserToken 读当前请求携带的会话令牌；没有则返回空串。
//
// 任何异常（存储未初始化、cookie 解码失败、类型不符）都按「没有令牌」处理 ——
// 调用方的语义是「这个请求有没有登录」，不是「为什么没有」。
func readUserToken(c *gin.Context) string {
	sess, err := userSession(c)
	if err != nil {
		return ""
	}
	raw, ok := sess.Values[userSessionTokenKey].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(raw)
}

// writeUserToken 写入会话令牌，rememberMe 决定 cookie 有效期（24h / 7d）。
func writeUserToken(c *gin.Context, token string, rememberMe bool) error {
	sess, err := userSession(c)
	if err != nil {
		return err
	}
	sess.Values[userSessionTokenKey] = token
	if rememberMe {
		sess.Options.MaxAge = userSessionRememberMaxAge
	} else {
		sess.Options.MaxAge = userSessionMaxAge
	}
	return sess.Save(c.Request, c.Writer)
}

// VisitorIdentityMiddleware 尽力解析访客会话，把 userID 挂到 gin context（**不阻断**）。
//
// 这是给访问面片段端点用的最小版本，与 attachUserSession 有三处刻意的差别：
//
//	· 只挂 user id，不挂会话对象 —— 片段层不需要昵称头像，也不需要撤销设备的行 id；
//	· **不做活跃时间续期**：片段请求量远大于页面请求，每个都 UPDATE 一次 user_sessions，
//	  是拿数据库写放大去换一个没人看的时间戳；
//	· 未登录不是错误 —— 访客未登录照样要看购物车计数与商品可用量，
//	  「必须登录」由具体片段能力自己声明（AuthVisitor）。
func VisitorIdentityMiddleware(svc usercontract.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil {
			c.Next()
			return
		}
		token := readUserToken(c)
		if token == "" {
			c.Next()
			return
		}
		if id, ok := svc.ResolveVisitorID(c.Request.Context(), token); ok && id != 0 {
			c.Set(usercontract.VisitorContextKey, id)
		}
		c.Next()
	}
}

// clearUserSession 清空访客 cookie 会话（登出：MaxAge=-1 让浏览器删掉它）。
func clearUserSession(c *gin.Context) error {
	sess, err := userSession(c)
	if err != nil {
		// 存储未初始化时无从清理，返回 nil（登出必须幂等）。
		return nil
	}
	sess.Values = make(map[interface{}]interface{})
	sess.Options.MaxAge = -1
	sess.Options.Path = "/"
	return sess.Save(c.Request, c.Writer)
}

// userCSRFStore 访客侧的 CSRF token 存取（实现在访客会话里，与后台互不干扰）。
//
// 必须独立：后台的 CSRF token 存在 gowp_session 中，访客写同一个键会把管理员的
// token 顶掉，于是后台下一个表单提交直接 403 —— 一个「登录一次前台就写不了后台」的怪现象。
type userCSRFStore struct{}

// Get 读取访客会话里的 CSRF token。
func (userCSRFStore) Get(c *gin.Context) string {
	sess, err := userSession(c)
	if err != nil {
		return ""
	}
	raw, ok := sess.Values[userCSRFKey].(string)
	if !ok {
		return ""
	}
	return raw
}

// Save 写入访客会话的 CSRF token。
func (userCSRFStore) Save(c *gin.Context, token string) error {
	sess, err := userSession(c)
	if err != nil {
		return err
	}
	sess.Values[userCSRFKey] = token
	return sess.Save(c.Request, c.Writer)
}
