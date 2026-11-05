package middleware

import "github.com/gin-gonic/gin"

func StaticCacheMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache, must-revalidate")
		c.Next()
	}
}
