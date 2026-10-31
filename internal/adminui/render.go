package adminui

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/view"
)

type PageResult = view.Result

func RenderPage(c *gin.Context, tpl string, view any) {
	status := http.StatusOK
	if r, ok := view.(PageResult); ok {
		if s := r.PageStatus(); s > 0 {
			status = s
		}
		if r.PageNoStore() {
			c.Header("Cache-Control", "no-store")
		}
	}
	c.HTML(status, tpl, PrepareView(c, view))
}

func RenderBare(c *gin.Context, tpl string, view any) {
	c.HTML(http.StatusOK, tpl, PrepareBareView(c, view))
}

func PrepareBareView(c *gin.Context, view any) gin.H {
	return PrepareBare(c, ViewMap(view))
}
