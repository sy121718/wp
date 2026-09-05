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
	defaultSessionSecret = "gowp-dev-session-secret-change-me-in-production"

	// 会话有效期（秒）：普通 24h，勾选记住我 7d。
	defaultSessionMaxAge    = 24 * 60 * 60
	rememberMeSessionMaxAge = 7 * 24 * 60 * 60
)

var (
	sessionMu    sync.RWMutex
	cookieStore  sessions.Store
	sessionReady bool
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

// weakSessionSecret 判断会话密钥是否为空或命中弱密钥集合。
func weakSessionSecret(secret string) bool {
	if secret == "" {
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
// Secure 属性按 server.mode 推导：release 时启用，避免生产环境明文 HTTP 泄露 cookie。
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
	if weakSessionSecret(secret) {
		if release {
			return errors.New("生产环境（server.mode=release）auth.session_secret 未配置或使用了弱默认值，拒绝启动：请配置高强度随机密钥")
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

	secure := release
	store := cookie.NewStore([]byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   defaultSessionMaxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	cookieStore = store
	sessionReady = true
	logger.Scene("init").Info("会话存储（Session + Cookie）初始化成功")
	return nil
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
