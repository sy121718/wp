package adminui

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/enums"
	"go_wp/pkg/response"
)

func Reject() builtin.PageRejectFunc {
	return func(c *gin.Context, _ int, key string) {
		c.Header("Cache-Control", "no-store")
		response.ErrorStatusAuto(c, "adminui", enums.AuthError(key))
	}
}
