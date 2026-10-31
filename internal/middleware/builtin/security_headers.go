package builtin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/unrolled/secure"
)

var securityHeaders = secure.New(secure.Options{

	CustomFrameOptionsValue: "SAMEORIGIN",

	ContentTypeNosniff: true,

	ReferrerPolicy: "strict-origin-when-cross-origin",

	ContentSecurityPolicy: "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob: https:; " +
		"font-src 'self' data:; " +
		"object-src 'none'; " +
		"frame-ancestors 'self'; " +
		"base-uri 'self'",
})

func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {

		c.Writer.Header().Del("X-Powered-By")

		if gin.Mode() != gin.ReleaseMode {
			c.Writer.Header().Set("X-GOWP-Debug", "1")
		}

		if err := securityHeaders.Process(c.Writer, c.Request); err != nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()

		c.Writer.Header().Del("X-Powered-By")
	}
}
