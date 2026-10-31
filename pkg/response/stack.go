package response

import (
	"context"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

func logInternalError(c *gin.Context, scene, msg string, err error) {
	if scene == "" {
		scene = "http"
	}
	entry := logger.SceneCtx(scene, requestContext(c))
	if c != nil && c.Request != nil {
		entry = entry.With("path", c.Request.URL.Path)
	}
	entry.With("stack", string(debug.Stack())).Error(err, msg)
}

func requestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

func LogInternalErrorf(c *gin.Context, scene, msg string, err error) {
	logInternalError(c, scene, msg, err)
}
