package builtin

import (
	"github.com/gin-gonic/gin"
)

// Recovery 返回 Gin 内置的崩溃恢复中间件。
//
// 当请求处理器发生 panic 时：
//   - 捕获 panic
//   - 记录堆栈日志
//   - 返回 500 Internal Server Error
//
// 这是所有中间件链的第一个，确保任何底层 panic 都不会导致进程崩溃。
// 适用位置：全局 engine.Use()，永远放在最前面。
func Recovery() gin.HandlerFunc {
	return gin.Recovery()
}
