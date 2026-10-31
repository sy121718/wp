package view

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/backpath"
)

func AfterWrite(c *gin.Context, target string) {
	if c == nil {
		return
	}
	if target = strings.TrimSpace(target); target == "" {
		return
	}
	c.Redirect(http.StatusSeeOther, target)
}

func GoTo(c *gin.Context, target string) {
	if c == nil {
		return
	}
	if target = strings.TrimSpace(target); target == "" {
		return
	}
	c.Redirect(http.StatusFound, target)
}

func Back(c *gin.Context, path string, keys ...string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	target, ok := backpath.Build(path, c.Request.URL.Query(), keys...)
	if !ok {
		return false
	}
	c.Redirect(http.StatusSeeOther, target)
	return true
}

func HXRedirect(c *gin.Context, target string) {
	if c == nil {
		return
	}
	if target = strings.TrimSpace(target); target == "" {
		return
	}
	c.Header("HX-Redirect", target)
	c.Status(http.StatusOK)
}
