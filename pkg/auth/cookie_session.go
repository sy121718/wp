package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	json "go_wp/pkg/jsonx"
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
	sessionName = "gowp_session"

	sessionUserKey = "auth_user"

	defaultSessionSecret = "gosky-dev-session-secret-change-me-in-production"

	defaultSessionMaxAge    = 24 * 60 * 60
	rememberMeSessionMaxAge = 7 * 24 * 60 * 60
)

var (
	sessionMu    sync.RWMutex
	cookieStore  sessions.Store
	sessionReady bool

	sessionSecret string
	sessionSecure bool
)

type CookieSession struct {
	SessionID string `json:"session_id"`
}

var weakSessionSecrets = map[string]struct{}{
	defaultSessionSecret:                  {},
	"your-session-secret-key-change-this": {},
	"your-secret-key":                     {},
}

const minSessionSecretLen = 32

func maxSessionCookieAge() int {
	if rememberMeSessionMaxAge > defaultSessionMaxAge {
		return rememberMeSessionMaxAge
	}
	return defaultSessionMaxAge
}

func applyCodecMaxAge(store sessions.Store) {
	if gs, ok := store.(interface{ MaxAge(int) }); ok {
		gs.MaxAge(maxSessionCookieAge())
	}
}

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

func SessionSecret() string {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return sessionSecret
}

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

func Ready() error {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	if !sessionReady || cookieStore == nil {
		return errors.New("会话存储未初始化")
	}
	return nil
}

func Close() error {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	cookieStore = nil
	sessionReady = false
	return nil
}

func NewSessionID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func sessionStart(c *gin.Context) (*gsessions.Session, error) {
	sessionMu.RLock()
	store := cookieStore
	sessionMu.RUnlock()

	if store == nil {
		return nil, errors.New("会话存储未初始化")
	}
	return store.Get(c.Request, sessionName)
}

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

func GetCookieSession(c *gin.Context) (*CookieSession, error) {
	session, err := sessionStart(c)
	if err != nil {

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

func ClearSession(c *gin.Context) error {
	session, err := sessionStart(c)
	if err != nil {

		return nil
	}
	session.Values = make(map[interface{}]interface{})
	session.Options.MaxAge = -1
	session.Options.Path = "/"
	return session.Save(c.Request, c.Writer)
}

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

func SetSessionValue(c *gin.Context, key string, value interface{}) error {
	session, err := sessionStart(c)
	if err != nil {
		return err
	}
	session.Values[key] = value
	return session.Save(c.Request, c.Writer)
}
