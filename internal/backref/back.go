package backref

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/backpath"
	"go_wp/pkg/logger"
)

const adminHomePath = "/admin"

const logScene = "shell"

func BackPath(c *gin.Context, path string, keys ...string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	source := c.Request.URL.Query()
	back, ok := backpath.Build(path, source, keys...)
	if !ok {
		logger.Scene(logScene).
			With("path", path).
			Warn("回跳路径不合法（非站内相对路径或超长），回控制面首页")
		return adminHomePath
	}

	for _, k := range keys {
		v := strings.TrimSpace(source.Get(k))
		if v == "" || backpath.KeepValue(v) {
			continue
		}
		logger.Scene(logScene).
			With("key", k).
			With("len", len(v)).
			Warn("回跳参数被丢弃（超长或含控制字符）")
	}
	return back
}
