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
	c.HTML(status, tpl, prepareView(c, data))
}

func RenderBare(c *gin.Context, tpl string, data any) {
	c.HTML(http.StatusOK, tpl, prepareBareView(c, data))
}

// RenderFragment 是 HTML 片段的唯一渲染入口：取词、CSRF 与权限事实由这里统一注入，
// 业务处理器只交数据，不再自己调 prepare*。
//
// 片段与整页共用同一份请求事实：模板里的 .csrf_token / .PermSet / .Buttons
// 在两种出口下都必须存在，否则片段会静默渲染成空。
func RenderFragment(c *gin.Context, tpl string, data any) {
	c.HTML(http.StatusOK, tpl, prepareView(c, data))
}

func prepareBareView(c *gin.Context, data any) gin.H {
	return prepareBare(c, ViewMap(data))
}
