package pagehttp

import (
	"testing"

	"github.com/gin-gonic/gin"
	"go_wp/internal/templates"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// newPageErrEngine 复刻生产接线：HTML 渲染、失败出口、成功出口与请求取词都在场。
func newPageErrEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = newPageErrTestRender(t)
	engine.Use(view.FailureMiddleware(view.RenderFailure))
	engine.Use(view.UseSuccess(view.NewSuccessWriter()))
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "page", err) }))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	return engine
}
