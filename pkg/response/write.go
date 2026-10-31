package response

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const HeaderMessage = "X-Message"

func writeResponse(c *gin.Context, status int, body Response) {

	if body.Message != "" {
		c.Header(HeaderMessage, encodeHeaderValue(body.Message))
	}
	c.JSON(status, body)
}

func encodeHeaderValue(s string) string {
	escaped := url.PathEscape(s)

	return strings.ReplaceAll(escaped, "+", "%2B")
}
