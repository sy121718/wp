package builtin

import (
	"net/http"
	"strings"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// RequestBodyLimitMiddleware 请求体大小限制中间件。
//
// 区别对待两类请求：
//   - 上传类请求（路径含 /upload 或 Content-Type 为 multipart/form-data）：使用 uploadBodyLimit
//   - 普通 API 请求：使用 requestBodyLimit
//
// 当请求 Content-Length 超过对应限制时，返回 413 Request Entity Too Large。
// 如果 limit <= 0，则不做限制直接放行。
//
// 参数说明：
//   - requestBodyLimit：普通请求体上限（字节）
//   - uploadBodyLimit：上传请求体上限（字节）
func RequestBodyLimitMiddleware(requestBodyLimit int64, uploadBodyLimit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := resolveRequestBodyLimit(c, requestBodyLimit, uploadBodyLimit)
		if limit <= 0 {
			c.Next()
			return
		}

		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, response.Response{
				Code:    http.StatusRequestEntityTooLarge,
				Message: "请求体过大",
			})
			return
		}

		c.Next()
	}
}

// resolveRequestBodyLimit 根据请求特征决定使用哪个限制值。
//
// 判定规则：
//   - Content-Type 以 multipart/form-data 开头 → 上传限制
//   - 请求路径包含 /upload → 上传限制
//   - 其他 → 普通限制
func resolveRequestBodyLimit(c *gin.Context, requestBodyLimit int64, uploadBodyLimit int64) int64 {
	if c == nil || c.Request == nil {
		return requestBodyLimit
	}

	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	path := strings.ToLower(strings.TrimSpace(c.Request.URL.Path))
	if strings.Contains(path, "/upload") || strings.HasPrefix(contentType, "multipart/form-data") {
		return uploadBodyLimit
	}
	return requestBodyLimit
}
