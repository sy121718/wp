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

// Recovery 返回自定义崩溃恢复中间件。
//
// 行为与 gin.Recovery 保持一致：
//   - 捕获 handler panic，进程不崩溃
//   - 连接中断（broken pipe / connection reset）时直接中止，不写响应
//   - 其余 panic 返回 500 Internal Server Error
//
// 差异：panic 值与堆栈通过 logger 以 JSON 结构化输出（scene=middleware），
// 不再走标准库 log 的纯文本。
//
// 这是所有中间件链的第一个，确保任何底层 panic 都不会导致进程崩溃。
// 适用位置：全局 engine.Use()，永远放在最前面。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			// 连接中断类 panic 直接中止，不写响应、不记录堆栈噪音。
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

			logger.Scene("middleware").With("stack", string(debug.Stack())).Error(err, "panic 恢复")
			c.AbortWithStatus(http.StatusInternalServerError)
		}()
		c.Next()
	}
}

// isBrokenPipe 判断 panic 值是否为连接中断类错误。
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
