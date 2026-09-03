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

// CSRFMiddleware 对写操作（POST/PUT/PATCH/DELETE）强制 CSRF token 校验。
//
// 校验逻辑：
//   - 请求头 X-CSRF-Token 或表单字段 csrf_token 必须与 cookie session 中保存的 token 一致
//   - 缺失或不一致 → 403 "CSRF 校验失败"
//
// GET/HEAD/OPTIONS 等安全方法直接放行。
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		token := c.GetHeader(csrfHeaderName)
		if token == "" {
			token = c.PostForm(csrfFormKey)
		}

		expected := getCSRFToken(c)
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
	if token := getCSRFToken(c); token != "" {
		return token, nil
	}

	token, err := newCSRFToken()
	if err != nil {
		return "", err
	}
	if err := auth.SetSessionValue(c, csrfSessionKey, token); err != nil {
		return "", err
	}
	return token, nil
}

// GetCSRFToken 供 handler / Jet 模板注入当前会话的 CSRF token（不存在时生成）。
// 用法：c.HTML(..., gin.H{"csrf_token": builtin.GetCSRFToken(c)})
func GetCSRFToken(c *gin.Context) (string, error) {
	return EnsureCSRFToken(c)
}

// getCSRFToken 从 cookie session 读取已保存的 CSRF token，不存在返回空串。
func getCSRFToken(c *gin.Context) string {
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
