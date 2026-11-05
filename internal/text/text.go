package text

import (
	"sync"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/view/jet"
)

var (
	userIDMu sync.RWMutex
	userIDOf func(*gin.Context) int64
)

// SetRequestUserID 由应用装配提供当前请求的主体 ID；
// 框架的错误日志只问这个回调，不反向依赖具体的会话中间件。
func SetRequestUserID(fn func(*gin.Context) int64) {
	userIDMu.Lock()
	defer userIDMu.Unlock()
	userIDOf = fn
}

func requestUserID(c *gin.Context) int64 {
	userIDMu.RLock()
	fn := userIDOf
	userIDMu.RUnlock()
	if fn == nil {
		return 0
	}
	return fn(c)
}

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
		With("user_id", requestUserID(c)).
		Error(err, "操作失败（非业务错误，只对外给归口文案）")
	return facing.Fact{Key: InternalKey, Fallback: InternalFallback}
}

func Lang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

func TranslateFunc(c *gin.Context) func(key, fallback string) string {
	return jet.TranslateFunc(Lang(c))
}
