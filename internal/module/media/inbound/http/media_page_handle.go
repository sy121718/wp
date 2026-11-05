package mediahttp

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"

	mediadto "go_wp/internal/module/media/dto"
	mediaenums "go_wp/internal/module/media/enums"
	pageview "go_wp/pkg/view"
)

func MediaPage(c *gin.Context) {

	pageview.Render(c, &mediadto.MediaPageResp{

		Title: mediaenums.Heading,
		JSVer: mediaLibJsVer(),
	})
}

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
