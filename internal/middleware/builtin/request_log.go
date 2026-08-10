package builtin

import (
	"errors"
	"fmt"
	"go_wp/pkg/logger"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogCaptureMiddleware 结构化 HTTP 请求日志中间件。
//
// 功能：
//   - 记录每次 HTTP 请求的方法、路径、状态码、客户端 IP、耗时
//   - 按场景写入 logger（scene="http"），方便日志分类检索
//   - 根据不同状态码级别选择 Error/Warn/Info 日志级别
//
// 日志级别规则：
//   - 有 gin.Error → Error 级别（记录具体错误信息）
//   - 状态码 >= 500 → Error 级别
//   - 状态码 >= 400 → Warn 级别
//   - 其他 → Info 级别
//
// 参数 enabled 控制是否启用，关闭时产生一个空中间件减少开销。
// 适用位置：全局 engine.Use() 或只对 /api 路由组挂载。
func RequestLogCaptureMiddleware(enabled bool) gin.HandlerFunc {
	if !enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	const scene = "http"
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method
		rawQuery := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		latency := time.Since(start)

		fields := map[string]interface{}{
			"method":     method,
			"path":       path,
			"status":     status,
			"client_ip":  c.ClientIP(),
			"latency_ms": latency.Milliseconds(),
		}
		if rawQuery != "" {
			fields["query"] = rawQuery
		}

		entry := logger.Scene(scene).WithFields(fields)
		if len(c.Errors) > 0 {
			errParts := make([]string, 0, len(c.Errors))
			for _, item := range c.Errors {
				errParts = append(errParts, item.Error())
			}
			joined := strings.Join(errParts, "; ")
			entry.With("errors", errParts).Error(errors.New(joined), "http request error")
			return
		}

		if status >= 500 {
			entry.Error(fmt.Errorf("http status %d", status), "http request failed")
			return
		}

		if status >= 400 {
			entry.Warn("http request warning")
			return
		}

		entry.Info("http request")
	}
}
