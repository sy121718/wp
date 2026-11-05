package builtin

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/auth"
	"go_wp/pkg/middleware"
)

const csrfSessionKey = "csrf_token"

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

// CSRFMiddleware 后台上行请求的参数表双提交校验。
func CSRFMiddleware() gin.HandlerFunc {
	return middleware.CSRFMiddleware(adminCSRFStore{})
}

// CSRFTokenStore 是框架原语的会话存储接口，保留别名供应用侧实现使用。
type CSRFTokenStore = middleware.CSRFTokenStore

// CSRFMiddlewareWith 用调用方 store 执行同一个校验。
func CSRFMiddlewareWith(store CSRFTokenStore) gin.HandlerFunc {
	return middleware.CSRFMiddleware(store)
}

// EnsureCSRFToken 读取或签发后台会话 token。
func EnsureCSRFToken(c *gin.Context) (string, error) {
	return middleware.EnsureCSRFToken(c, adminCSRFStore{})
}

// EnsureCSRFTokenWith 用调用方 store 读取或签发 token。
func EnsureCSRFTokenWith(c *gin.Context, store CSRFTokenStore) (string, error) {
	return middleware.EnsureCSRFToken(c, store)
}

// GetCSRFToken 是 EnsureCSRFToken 的历史名称。
func GetCSRFToken(c *gin.Context) (string, error) {
	return EnsureCSRFToken(c)
}

// RotateCSRFToken 丢弃旧 token 并签发新值。
func RotateCSRFToken(c *gin.Context) (string, error) {
	return middleware.RotateCSRFToken(c, adminCSRFStore{})
}

// RotateCSRFTokenWith 用调用方 store 轮换 token。
func RotateCSRFTokenWith(c *gin.Context, store CSRFTokenStore) (string, error) {
	return middleware.RotateCSRFToken(c, store)
}
