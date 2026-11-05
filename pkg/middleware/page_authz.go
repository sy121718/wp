package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/enums"
	"go_wp/pkg/view"
)

// PageRejectFunc 是页面鉴权失败时的受控拒绝出口。
type PageRejectFunc func(c *gin.Context, status int, key string)

// PageCasbinMiddleware 校验页面权限对象（方法为 GET）。
func PageCasbinMiddleware(obj string, reject PageRejectFunc) gin.HandlerFunc {
	return PageCasbinMiddlewareAs(obj, http.MethodGet, reject)
}

// PageCasbinMiddlewareAs 用显式方法校验页面权限对象。
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

// PageAuthz 是页面 GET 的鉴权中间件：失败交共享失败出口。
//
// 应用只传权限对象/方法，不需要为后台再写一个拒绝器副本。
func PageAuthz(obj string) gin.HandlerFunc {
	return PageAuthzAs(obj, http.MethodGet)
}

// PageAuthzAs 是页面鉴权的显式方法版本（页面可复用别的操作的权限）。
func PageAuthzAs(obj, act string) gin.HandlerFunc {
	return PageCasbinMiddlewareAs(obj, act, func(c *gin.Context, status int, key string) {
		view.FailScene(c, "authz", enums.AuthError(key))
	})
}
