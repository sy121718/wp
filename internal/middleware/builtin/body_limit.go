package builtin

import (
	"net/http"
	"strings"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

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

			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}

		c.Next()
	}
}

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
