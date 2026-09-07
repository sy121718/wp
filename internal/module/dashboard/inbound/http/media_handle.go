package dashboardhttp

// media_handle.go — 媒体库页面入口（左树右库）。
// 从 dashboard_handle.go 拆出：媒体库页面域与工作台/预览解耦。

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
)

// MediaPage 媒体库页面（左树右库：无限级分类筛选 + WP 式媒体网格/列表）。
// 页面骨架由模板渲染，数据与交互由 media-admin.js 驱动（复用 /api/media/*）。
func (h *Handle) MediaPage(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/media", withCSRF(c, gin.H{
		"title": "媒体库",
		"menu":  "media",
		"jsVer": mediaLibJsVer(),
	}))
}

// mediaLibJsVer 媒体库脚本缓存版本（media-lib.js / media-admin.js 中较新的 mtime）。
func mediaLibJsVer() string {
	latest := int64(0)
	for _, name := range []string{"media-lib.js", "media-admin.js"} {
		if fi, err := os.Stat(filepath.Join("internal", "templates", "static", "js", name)); err == nil && fi.ModTime().Unix() > latest {
			latest = fi.ModTime().Unix()
		}
	}
	if latest > 0 {
		return strconv.FormatInt(latest, 10)
	}
	return "0"
}
