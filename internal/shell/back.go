package shell

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/backpath"
)

const backValueMaxBytes = backpath.MaxQueryValueBytes

// BackPath 保留既有签名：安全回跳规则已归 pkg/backpath，这里只给应用默认回跳路径。
func BackPath(c *gin.Context, path string, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	return backpath.FromRequest(c.Request.URL.Query(), path, adminHomePath, "shell", keys...)
}
