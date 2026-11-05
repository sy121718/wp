package adminui

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/view"
)

type PageResult = view.Result

func RenderPage(c *gin.Context, tpl string, data any) {
	status := http.StatusOK
	if r, ok := data.(PageResult); ok {
		if s := r.PageStatus(); s > 0 {
			status = s
		}
		if r.PageNoStore() {
			c.Header("Cache-Control", "no-store")
		}
	}
	c.HTML(status, tpl, PrepareView(c, data))
}

func RenderBare(c *gin.Context, tpl string, data any) {
	c.HTML(http.StatusOK, tpl, PrepareBareView(c, data))
}

func PrepareBareView(c *gin.Context, data any) gin.H {
	return PrepareBare(c, ViewMap(data))
}
