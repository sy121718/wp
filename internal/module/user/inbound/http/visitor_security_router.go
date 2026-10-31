package userhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	usercontract "go_wp/internal/module/user/contract"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/response"
)

func VisitorCSRFMiddleware() gin.HandlerFunc {
	check := builtin.CSRFMiddlewareWith(userCSRFStore{})
	return func(c *gin.Context) {
		token, err := builtin.EnsureCSRFTokenWith(c, userCSRFStore{})
		if err != nil {
			response.ErrorWithMessage(c, http.StatusInternalServerError, userenums.ErrInternal)
			c.Abort()
			return
		}
		c.Set(usercontract.VisitorCSRFContextKey, token)
		c.Header("X-CSRF-Token", token)
		c.Header("Cache-Control", "no-store")
		check(c)
	}
}
