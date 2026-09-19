// Package auth 的 Cookie 会话存储：认证载体从 JWT 迁移为 Session + Cookie。
//
// 职责划分：
//   - 本文件：Cookie 会话（gin-contrib/sessions + cookie store）——只管「你是谁」的认证载体，
//     承载最小认证信息（user_id / username / session_id / issued_at），HTMX 请求自动携带。
//   - session.go：Redis 用户会话——封禁标记、在线心跳、用户资料，保留不变。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"go_wp/pkg/cache"
	"go_wp/pkg/logger"
	"go_wp/pkg/sitehttps"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	gsessions "github.com/gorilla/sessions"
	"github.com/spf13/viper"
)

const (
	// sessionName 认证 cookie 名称，HTMX 请求自动携带。
	sessionName = "gowp_session"

	// cookie 会话中保存的键。
	sessionUserKey = "auth_user" // CookieSession 的 JSON 串

	// 开发默认 secret：仅用于本地开发，生产必须通过配置覆盖。
	defaultSessionSecret = "gosky-dev-session-secret-change-me-in-production"

	// 会话有效期（秒）：普通 24h，勾选记住我 7d。
	defaultSessionMaxAge    = 24 * 60 * 60
	rememberMeSessionMaxAge = 7 * 24 * 60 * 60
)

var (
	sessionMu    sync.RWMutex
	cookieStore  sessions.Store
	sessionReady bool
	// sessionSecret / sessionSecure 保留 Init 时的实际取值，
	// 供 NamedCookieStore 为别的身份域创建**同源密钥、不同 cookie 名**的存储。
	sessionSecret string
	sessionSecure bool
)

// CookieSession 认证 cookie 中保存的最小会话信息。
//
// 只放认证所需的最小字段，用户资料（头像/邮箱/部门等）仍走 Redis（session.go）。
type CookieSession struct {
	UserID    uint64 `json:"user_id"`
	Username  string `json:"username"`
	SessionID string `json:"session_id"`
	IssuedAt  int64  `json:"issued_at"` // 会话建立时间戳（秒），用于封禁判断
}

// weakSessionSecrets 生产环境（server.mode=release）禁止使用的弱密钥集合，
// 包含开发默认值与常见示例密钥。
var weakSessionSecrets = map[string]struct{}{
	defaultSessionSecret:                  {},
	"your-session-secret-key-change-this": {},
	"your-secret-key":                     {},
}

const minSessionSecretLen = 32

// maxSessionCookieAge 返回两种会话有效期中的较大者。
//
// securecookie 的 MaxAge 是**每个 codec 一个值**，而 cookie 有 24h（普通）与 7d（记住我）
// 两种有效期，所以只能取较大者：取 24h 会让勾了「记住我」的用户在第 25 小时被**签名层**
// 判过期 —— 浏览器还带着 cookie、Redis 会话也还在，但解码直接失败，表现为
// 「勾了记住我照样被踢下线」。
func maxSessionCookieAge() int {
	if rememberMeSessionMaxAge > defaultSessionMaxAge {
		return rememberMeSessionMaxAge
	}
	return defaultSessionMaxAge
}

// applyCodecMaxAge 把会话有效期显式转发给 securecookie 编解码器。
//
// 为什么需要这一步：gin-contrib 的 store.Options() 实现是
//
//	c.CookieStore.Options = options.ToGorillaOptions()
//
// —— 只赋 Options 字段，**不经过 CookieStore.MaxAge()**。而 securecookie 的时间戳
// 校验窗口正是在 MaxAge() 里逐个 codec 设置的，因此它一直停在 gorilla 的默认值
// （NewCookieStore 里的 86400*30，30 天），与配置的 24h / 7d 无关。
//
// 后果：浏览器会按时删掉 cookie，但任何被留存下来的 cookie 值（日志、代理、备份、
// 手工复制）在签发后 30 天内仍能通过验签。当前各身份域都有服务端校验兜住
// （admin / 访客查 Redis、购物车自校验时间戳），所以这不是可利用漏洞，
// 而是一条「只在服务端不查时才会显形的过期窗口」—— 显式转发即可消除。
func applyCodecMaxAge(store sessions.Store) {
	if gs, ok := store.(interface{ MaxAge(int) }); ok {
		gs.MaxAge(maxSessionCookieAge())
	}
}

// weakSessionSecret 判断会话密钥是否为空、过短或命中弱密钥集合。
func weakSessionSecret(secret string, release bool) bool {
	if secret == "" {
		return true
	}
	if release && len(secret) < minSessionSecretLen {
		return true
	}
	_, ok := weakSessionSecrets[secret]
	return ok
}

// Init 初始化 Cookie 会话存储。
//
// 从配置读取 auth.session_secret：
//   - M6：server.mode=release 时，密钥为空或命中弱密钥集合直接返回错误拒绝启动；debug 模式维持告警。
//   - H3 防御：配置已启用 redis 但 cache 组件未就绪时返回错误，避免「启动正常、登录后全站 503」。
//
// Secure 属性的真源是 pkg/sitehttps（server.site_https 显式声明 > server.mode 推导）。
//
// 为什么不再直接看 server.mode：反代终结 TLS 是自托管最常见的部署形态，此时
// 进程自身跑在 HTTP 上，"非 release 就不带 Secure" 的口径会让 HTTPS 站点的
// admin_session / 访客会话 cookie 少了 Secure —— 而 debug 模式又恰恰是最常被
// 误配到公网的那种（见 cmd/main.go 的 listenAddr 护栏）。
func Init(v *viper.Viper) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	if sessionReady {
		return nil
	}

	configured := ""
	release := false
	if v != nil {
		configured = strings.TrimSpace(v.GetString("auth.session_secret"))
		release = strings.EqualFold(strings.TrimSpace(v.GetString("server.mode")), "release")
	}

	secret := configured
	if secret == "" {
		secret = defaultSessionSecret
	}

	// M6：release 模式弱密钥 fail-fast；debug 模式仅告警，保持本地开发零配置可用。
	if weakSessionSecret(secret, release) {
		if release {
			return errors.New("生产环境（server.mode=release）auth.session_secret 未配置、过短（<32 字符）或使用了弱默认值，拒绝启动：请配置高强度随机密钥")
		}
		if configured == "" {
			logger.Scene("init").Warn("auth.session_secret 未配置，使用开发默认值，生产环境必须修改")
		} else {
			logger.Scene("init").Warn("auth.session_secret 为已知弱值，生产环境必须更换")
		}
	}

	// H3 防御：认证会话（session.go）、封禁标记、在线心跳均走 pkg/cache（Redis）。
	// 配置声称启用 redis 但 cache 组件未就绪（如组件编排顺序被破坏）时拒绝启动。
	// redis.enabled=false 的完整 fail-fast 校验由组件编排层在 Init 后调用 RequireSessionStorage 完成。
	if v != nil && v.GetBool("redis.enabled") && !cache.IsInited() {
		return errors.New("认证会话存储不可用：cache（Redis）组件未就绪，请检查 redis 配置与组件初始化顺序")
	}

	secure := sitehttps.Enabled()
	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   defaultSessionMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	applyCodecMaxAge(store)

	cookieStore = store
	sessionSecret = secret
	sessionSecure = secure
	sessionReady = true
	logger.Scene("init").Info("会话存储（Session + Cookie）初始化成功")
	return nil
}

// SessionSecret 返回 Init 时确定的会话签名密钥（未初始化时为空串）。
//
// 供**同一部署内的其它签名用途**复用 —— 购物车 cookie 的 HMAC 就取它。
// 理由与 NamedCookieStore 同一条：签名密钥属于部署，不属于某个身份域；
// 再造一个配置项只会制造「两个密钥、轮换时改一个漏一个」的机会，
// 而漏掉的那个会让购物车 cookie 在某次密钥轮换后集体失效（表现是「所有人的购物车都空了」）。
func SessionSecret() string {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return sessionSecret
}

// NamedCookieStore 按指定的 cookie 名创建会话存储，供**第二身份域**使用。
//
// 为什么需要：一个站点会有多个互不相干的登录态（管理后台、访客账号、将来的客户账号）。
// 它们必须用不同的 cookie 名 —— 同一个 cookie 只能存一份会话，
// 后登录的一方会把先登录的一方顶掉（后台管理员会莫名其妙被踢出去）。
// 同理，Redis 那边的会话 key 也必须分开，见 user 模块的会话说明。
//
// 密钥与后台会话**同源**（同一个 auth.session_secret）：签名密钥属于部署，不属于某个身份域。
// 隔离靠 cookie 名，不靠「各用一份密钥」—— 后者只会让运维多管一个密钥、
// 并在轮换时漏掉其中一个。隔离后的 cookie 之间不会互相解码，因为名不同。
func NamedCookieStore(name string) (sessions.Store, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("cookie 名不能为空")
	}

	sessionMu.RLock()
	secret := sessionSecret
	secure := sessionSecure
	ready := sessionReady
	existing := cookieStore
	sessionMu.RUnlock()

	if !ready || secret == "" {
		return nil, errors.New("会话存储未初始化（请先调用 auth.Init）")
	}
	// 同名直接复用：避免同一身份域在不同装配点得到两个 store 实例
	//（虽然后果只是多解析一次，但两个实例意味着「Options 可能不一致」的隐患）。
	if name == sessionName && existing != nil {
		return existing, nil
	}

	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   defaultSessionMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	applyCodecMaxAge(store)
	return store, nil
}

// Ready 检查会话存储是否已初始化。
func Ready() error {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	if !sessionReady || cookieStore == nil {
		return errors.New("会话存储未初始化")
	}
	return nil
}

// Close 关闭会话存储并清空运行时状态。
func Close() error {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	cookieStore = nil
	sessionReady = false
	return nil
}

// NewSessionID 生成新的会话 ID（16 字节随机数的 hex 编码）。
// 用于把 cookie 会话与 Redis 用户会话（user:session:{id}）绑定。
func NewSessionID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// sessionStart 获取当前请求的底层 cookie 会话（gorilla 按请求缓存，同请求多次调用返回同一对象）。
func sessionStart(c *gin.Context) (*gsessions.Session, error) {
	sessionMu.RLock()
	store := cookieStore
	sessionMu.RUnlock()

	if store == nil {
		return nil, errors.New("会话存储未初始化")
	}
	return store.Get(c.Request, sessionName)
}

// SaveCookieSession 把最小认证会话写入 cookie，rememberMe 决定有效期（24h / 7d）。
func SaveCookieSession(c *gin.Context, cs *CookieSession, rememberMe bool) error {
	if cs == nil {
		return errors.New("会话信息不能为空")
	}
	session, err := sessionStart(c)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(cs)
	if err != nil {
		return err
	}
	session.Values[sessionUserKey] = string(payload)

	if rememberMe {
		session.Options.MaxAge = rememberMeSessionMaxAge
	} else {
		session.Options.MaxAge = defaultSessionMaxAge
	}
	return session.Save(c.Request, c.Writer)
}

// GetCookieSession 从 cookie 读取认证会话。
//
// 未登录 / cookie 缺失 / 解码失败 / 结构非法时返回 (nil, nil)，由调用方按未登录处理。
func GetCookieSession(c *gin.Context) (*CookieSession, error) {
	session, err := sessionStart(c)
	if err != nil {
		// 会话存储未初始化：视为未登录，避免中间件误报 500。
		return nil, nil
	}

	raw, ok := session.Values[sessionUserKey].(string)
	if !ok || raw == "" {
		return nil, nil
	}

	var cs CookieSession
	if err := json.Unmarshal([]byte(raw), &cs); err != nil {
		return nil, nil
	}
	return &cs, nil
}

// ClearSession 清空 cookie 会话（退出登录时调用），通过 MaxAge=-1 删除 cookie。
func ClearSession(c *gin.Context) error {
	session, err := sessionStart(c)
	if err != nil {
		// 存储未初始化时无从清理，直接返回 nil（登出幂等）。
		return nil
	}
	session.Values = make(map[interface{}]interface{})
	session.Options.MaxAge = -1
	session.Options.Path = "/"
	return session.Save(c.Request, c.Writer)
}

// GetSessionValue 读取 cookie 会话中的指定键（供 CSRF 等扩展使用）。
func GetSessionValue(c *gin.Context, key string) (interface{}, bool) {
	session, err := sessionStart(c)
	if err != nil {
		return nil, false
	}
	v, ok := session.Values[key]
	if !ok || v == nil {
		return nil, false
	}
	return v, true
}

// SetSessionValue 写入并保存 cookie 会话中的指定键（供 CSRF 等扩展使用）。
func SetSessionValue(c *gin.Context, key string, value interface{}) error {
	session, err := sessionStart(c)
	if err != nil {
		return err
	}
	session.Values[key] = value
	return session.Save(c.Request, c.Writer)
}
