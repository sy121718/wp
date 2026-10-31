package shell

import (
	"go_wp/internal/templates"

	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func TranslateFor(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

const adminHomePath = "/admin"

func requestURI(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return adminHomePath
	}
	return uri
}
