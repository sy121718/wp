package view

import (
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/response"
)

type Renderer func(c *gin.Context, tpl string, data any)

const (
	ctxTplKey      = "view.template"
	ctxRendererKey = "view.renderer"
)

func Slot(tpl string, render Renderer) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ctxTplKey, tpl)
		c.Set(ctxRendererKey, render)
		c.Next()
	}
}

func UseRenderer(render Renderer) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ctxRendererKey, render)
		c.Next()
	}
}

func RenderTemplate(c *gin.Context, tpl string, data any) {
	c.Set(ctxTplKey, tpl)
	Render(c, data)
}

func Render(c *gin.Context, data any) {
	render, tpl, ok := slotOf(c)
	if !ok {
		FailScene(c, "view", errors.New("视图出口未装配：路由缺渲染器"))
		return
	}
	if data == nil {
		FailScene(c, sceneOf(tpl), errors.New("页面视图为空（契约：视图恒非 nil）"))
		return
	}
	render(c, tpl, data)
}

func Fail(c *gin.Context, err error) {
	_, tpl, _ := slotOf(c)
	FailScene(c, sceneOf(tpl), err)
}

func FailScene(c *gin.Context, scene string, err error) {
	FailStatus(c, response.ErrorStatus(err), scene, err)
}

func sceneOf(tpl string) string {
	parts := strings.Split(tpl, "/")
	if len(parts) >= 2 && parts[0] == "admin" {
		return parts[1]
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "view"
}

func slotOf(c *gin.Context) (Renderer, string, bool) {
	raw, ok := c.Get(ctxRendererKey)
	if !ok {
		return nil, "", false
	}
	render, ok := raw.(Renderer)
	if !ok || render == nil {
		return nil, "", false
	}
	tpl, _ := c.Get(ctxTplKey)
	name, _ := tpl.(string)
	return render, name, true
}
