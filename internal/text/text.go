package text

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const InternalKey = response.InternalKey

const InternalFallback = response.InternalFallback

func FactOf(c *gin.Context, scene string, err error) facing.Fact {
	if err == nil {
		return facing.Fact{}
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	logger.Scene(scene).
		With("path", RequestURI(c)).
		With("user_id", builtin.CurrentUserID(c)).
		Error(err, "操作失败（非业务错误，只对外给归口文案）")
	return facing.Fact{Key: InternalKey, Fallback: InternalFallback}
}

func Lang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

func TranslateFunc(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(Lang(c))
}
