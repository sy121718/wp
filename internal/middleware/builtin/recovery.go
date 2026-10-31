package builtin

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			if rec == http.ErrAbortHandler {
				c.Abort()
				return
			}

			if isBrokenPipe(rec) {
				c.Abort()
				return
			}

			var err error
			if e, ok := rec.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("%v", rec)
			}

			logger.SceneCtx("middleware", c.Request.Context()).
				With("stack", string(debug.Stack())).
				Error(err, "panic 恢复")
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}

func isBrokenPipe(rec any) bool {
	e, ok := rec.(error)
	if !ok {
		return false
	}
	var ne *net.OpError
	if errors.As(e, &ne) {
		var se *os.SyscallError
		if errors.As(ne.Err, &se) {
			msg := strings.ToLower(se.Error())
			return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
		}
	}
	return false
}
