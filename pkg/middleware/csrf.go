package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/view"
)

const (
	// CSRFHeaderName 是双提交校验读取 token 的请求头。
	CSRFHeaderName = "X-CSRF-Token"
	// CSRFContextKey 允许应用把已签发的 token 放进上下文供模板读取。
	CSRFContextKey = "csrf_token"
)

// CSRFTokenStore 抽象 token 的会话存储。
// 框架只提供原语，具体会话实现由应用注入。
type CSRFTokenStore interface {
	Get(c *gin.Context) string
	Save(c *gin.Context, token string) error
}

// CSRFMiddleware 用调用方提供的 store 做双提交校验。
func CSRFMiddleware(store CSRFTokenStore) gin.HandlerFunc {
	if store == nil {
		panic("middleware: CSRFMiddleware 需要非空 store")
	}
	return func(c *gin.Context) {
		if !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		token := c.GetHeader(CSRFHeaderName)
		if token == "" {
			token = c.PostForm(CSRFContextKey)
		}

		expected := store.Get(c)
		if token == "" || expected == "" || !secureCompare(token, expected) {
			view.FailStatus(c, http.StatusForbidden, "csrf", facing.New(enums.ErrPermissionDenied))
			c.Abort()
			return
		}

		c.Next()
	}
}

// EnsureCSRFToken 读取或签发 token。
func EnsureCSRFToken(c *gin.Context, store CSRFTokenStore) (string, error) {
	if store == nil {
		panic("middleware: EnsureCSRFToken 需要非空 store")
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

// RotateCSRFToken 丢弃旧 token 并签发新值。
func RotateCSRFToken(c *gin.Context, store CSRFTokenStore) (string, error) {
	if store == nil {
		panic("middleware: RotateCSRFToken 需要非空 store")
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
