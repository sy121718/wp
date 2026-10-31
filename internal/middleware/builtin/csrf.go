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
	csrfSessionKey = "csrf_token"

	csrfHeaderName = "X-CSRF-Token"

	csrfFormKey = "csrf_token"
)

type CSRFTokenStore interface {
	Get(c *gin.Context) string

	Save(c *gin.Context, token string) error
}

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

func CSRFMiddleware() gin.HandlerFunc {
	return CSRFMiddlewareWith(adminCSRFStore{})
}

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

func EnsureCSRFToken(c *gin.Context) (string, error) {
	return EnsureCSRFTokenWith(c, adminCSRFStore{})
}

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

func GetCSRFToken(c *gin.Context) (string, error) {
	return EnsureCSRFToken(c)
}

func RotateCSRFToken(c *gin.Context) (string, error) {
	return RotateCSRFTokenWith(c, adminCSRFStore{})
}

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

func newCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
