package builtin

import (
	"errors"
	"fmt"
	"go_wp/pkg/logger"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var sensitiveQueryKeys = map[string]struct{}{
	"token": {}, "code": {}, "key": {}, "password": {}, "secret": {},
	"csrf_token": {}, "reset_token": {}, "activation_key": {},
}

func redactRawQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[invalid-query]"
	}
	for k := range vals {
		if _, ok := sensitiveQueryKeys[strings.ToLower(k)]; ok {
			vals.Set(k, "[redacted]")
		}
	}
	return vals.Encode()
}

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

		fields := map[string]any{
			"method":     method,
			"path":       path,
			"status":     status,
			"client_ip":  c.ClientIP(),
			"latency_ms": latency.Milliseconds(),
		}
		if rawQuery != "" {
			fields["query"] = redactRawQuery(rawQuery)
		}

		entry := logger.SceneCtx(scene, c.Request.Context()).WithFields(fields)
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
