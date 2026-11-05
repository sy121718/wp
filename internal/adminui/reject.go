package adminui

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/enums"
	"go_wp/pkg/view"
)

func Reject() builtin.PageRejectFunc {
	return func(c *gin.Context, _ int, key string) {
		c.Header("Cache-Control", "no-store")
		view.FailScene(c, "adminui", enums.AuthError(key))
	}
}
