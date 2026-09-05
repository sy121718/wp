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
// 限制策略（不信任 Content-Length 头）：
//   - ContentLength > limit → 直接 413 Request Entity Too Large
//   - ContentLength < 0（chunked / 未知长度）→ 用 http.MaxBytesReader 包裹 Body，
//     读取时强制截断到 limit；超过 limit 的 Read 返回 *http.MaxBytesError，
//     由读取方（handler）判定为 413，不会静默读取超限数据。
//     不用 io.LimitReader 的原因：它静默截断，handler 会拿到「看似完整实则截断」
//     的数据而不知情，破坏请求体完整性；MaxBytesReader 显式报错更安全。
//
// 当 limit <= 0 时不做限制直接放行。
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

		if c.Request.ContentLength < 0 {
			// chunked 或未知长度：Content-Length 不可信，读取时强制限制。
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
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
