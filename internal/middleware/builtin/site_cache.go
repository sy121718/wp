package builtin

import "github.com/gin-gonic/gin"

func SiteCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=0, must-revalidate")
		c.Next()
	}
}
