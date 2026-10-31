package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
)

func PageAuthz(obj string) gin.HandlerFunc {
	return builtin.PageCasbinMiddleware(obj, Reject())
}

func PageAuthzAs(obj, act string) gin.HandlerFunc {
	return builtin.PageCasbinMiddlewareAs(obj, act, Reject())
}
