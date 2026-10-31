package builtin

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type PageRejectFunc func(c *gin.Context, status int, key string)

func PageCasbinMiddleware(obj string, reject PageRejectFunc) gin.HandlerFunc {
	return PageCasbinMiddlewareAs(obj, http.MethodGet, reject)
}

func PageCasbinMiddlewareAs(obj, act string, reject PageRejectFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		status, key := CasbinDecide(c, obj, act)
		if status == 0 {
			c.Next()
			return
		}
		reject(c, status, key)
		c.Abort()
	}
}
