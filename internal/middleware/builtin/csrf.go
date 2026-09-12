package builtin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"

	"go_wp/pkg/auth"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

const (
	// csrfSessionKey CSRF token 在 cookie session 中的存储键。
	csrfSessionKey = "csrf_token"
	// csrfHeaderName 校验时的请求头名。
	csrfHeaderName = "X-CSRF-Token"
	// csrfFormKey 校验时的表单字段名（Jet 模板注入隐藏域）。
	csrfFormKey = "csrf_token"
)

// CSRFTokenStore CSRF token 的存取方式。
//
// 存在的理由：CSRF 校验逻辑与「token 存在哪个会话里」是两件事。本项目有**两个身份域**
// （管理后台 gowp_session、访客账号 gowp_user_session），两者各有一个 cookie 会话。
// 把校验逻辑写死在 admin 的会话上，访客侧就只能复制一份 —— 而复制出来的那份
// 迟早会在「常量时间比较」「登录后轮换」这类细节上与原本分叉。
//
// 默认实现读取管理后台的会话（CSRFMiddleware 等既有入口行为完全不变）。
type CSRFTokenStore interface {
	// Get 读取当前会话的 CSRF token，不存在返回空串。
	Get(c *gin.Context) string
	// Save 写入当前会话的 CSRF token。
	Save(c *gin.Context, token string) error
}

// adminCSRFStore 管理后台的 token 存取（默认实现）。
type adminCSRFStore struct{}

func (adminCSRFStore) Get(c *gin.Context) string {
	v, ok := auth.GetSessionValue(c, csrfSessionKey)
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func (adminCSRFStore) Save(c *gin.Context, token string) error {
	return auth.SetSessionValue(c, csrfSessionKey, token)
}

// CSRFMiddleware 对写操作（POST/PUT/PATCH/DELETE）强制 CSRF token 校验。
//
// 校验逻辑：
//   - 请求头 X-CSRF-Token 或表单字段 csrf_token 必须与 cookie session 中保存的 token 一致
//   - 缺失或不一致 → 403 "CSRF 校验失败"
//
// GET/HEAD/OPTIONS 等安全方法直接放行。
func CSRFMiddleware() gin.HandlerFunc {
	return CSRFMiddlewareWith(adminCSRFStore{})
}

// CSRFMiddlewareWith 用指定的 token 存取器构造 CSRF 中间件（供访客账号等第二身份域使用）。
func CSRFMiddlewareWith(store CSRFTokenStore) gin.HandlerFunc {
	if store == nil {
		store = adminCSRFStore{}
	}
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		token := c.GetHeader(csrfHeaderName)
		if token == "" {
			token = c.PostForm(csrfFormKey)
		}

		expected := store.Get(c)
		if token == "" || expected == "" || !secureCompare(token, expected) {
			response.ErrorWithMessage(c, 403, "CSRF 校验失败")
			c.Abort()
			return
		}

		c.Next()
	}
}

// EnsureCSRFToken 获取当前会话的 CSRF token，不存在时生成并写入 session 后返回。
// 登录成功与渲染表单时调用，保证后续写操作有可校验的 token。
func EnsureCSRFToken(c *gin.Context) (string, error) {
	return EnsureCSRFTokenWith(c, adminCSRFStore{})
}

// EnsureCSRFTokenWith 同上，使用指定的 token 存取器。
func EnsureCSRFTokenWith(c *gin.Context, store CSRFTokenStore) (string, error) {
	if store == nil {
		store = adminCSRFStore{}
	}
	if token := store.Get(c); token != "" {
		return token, nil
	}
	token, err := newCSRFToken()
	if err != nil {
		return "", err
	}
	if err := store.Save(c, token); err != nil {
		return "", err
	}
	return token, nil
}

// GetCSRFToken 供 handler / Jet 模板注入当前会话的 CSRF token（不存在时生成）。
// 用法：c.HTML(..., gin.H{"csrf_token": builtin.GetCSRFToken(c)})
func GetCSRFToken(c *gin.Context) (string, error) {
	return EnsureCSRFToken(c)
}

// RotateCSRFToken 强制生成新 CSRF token 并覆盖会话旧值（登录成功后调用）。
//
// 区别于 EnsureCSRFToken（存在即复用）：登录前的匿名会话可能已被攻击者
// 预置 CSRF token（如子域 Set-Cookie 注入），复用旧值会让攻击者预知登录后的
// CSRF token；登录成功必须轮换，使预置 token 失效。
func RotateCSRFToken(c *gin.Context) (string, error) {
	return RotateCSRFTokenWith(c, adminCSRFStore{})
}

// RotateCSRFTokenWith 同上，使用指定的 token 存取器。
func RotateCSRFTokenWith(c *gin.Context, store CSRFTokenStore) (string, error) {
	if store == nil {
		store = adminCSRFStore{}
	}
	token, err := newCSRFToken()
	if err != nil {
		return "", err
	}
	if err := store.Save(c, token); err != nil {
		return "", err
	}
	return token, nil
}

// newCSRFToken 生成 32 字节随机数的 hex 编码作为 CSRF token。
func newCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// secureCompare 常量时间比较，避免时序侧信道。
func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// isUnsafeMethod 判断是否为需要 CSRF 校验的写方法。
func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
