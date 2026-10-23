package response

// stack.go — 内部错误日志的**堆栈**与 request_id 归口。
//
// 要解决的问题：`pkg/facing` 的错误是「key + 兜底 + 状态码」，对外很好；但 500 的日志里
// 只有一句话，没有出错位置。排查时只能读代码猜哪一步返回的 err —— 而在 Go 里重新包装
// 错误（fmt.Errorf("%w")）会让调用栈信息彻底消失。
//
// 取舍：**不做错误包装链**（`errors.WithStack` 要改几百处 service，且漏一处即永久丢失），
// 而是在「内部错误出口」这一个位置抓 `debug.Stack()`。出口只有三处（JSON 500、后台页面
// 500、后台页面 400 的意外分支），改一处等于全局生效。
//
// 堆栈只进日志、绝不进响应体：它是内部结构（文件路径、函数名、行号）。

import (
	"context"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

// logInternalError 内部错误出口的唯一日志写法：err + 堆栈 + request_id。
//
// 三样缺一不可：
//   - err：发生了什么；
//   - stack：在哪发生的（本文件的全部理由）；
//   - request_id：是哪一次请求（由 ctx 自动带来，见 internal/middleware/builtin）。
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

// requestContext 从 gin.Context 安全取 request context（测试里可能是裸 &gin.Context{}）。
func requestContext(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}

// LogInternalErrorf 供同族出口（后台页面错误）复用的日志写法。
//
// 导出而不是让 adminui 自己再写一遍 logger 调用：堆栈与 request_id 的取法只该有一份，
// 否则「JSON 500 有堆栈、页面 500 没有」这种半吊子状态没有任何测试会发现。
func LogInternalErrorf(c *gin.Context, scene, msg string, err error) {
	logInternalError(c, scene, msg, err)
}
