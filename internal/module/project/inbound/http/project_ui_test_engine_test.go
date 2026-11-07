package projecthttp

import (
	"testing"

	"github.com/gin-gonic/gin"
	"go_wp/internal/templates"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// newProjectUITestEngine 复刻生产接线：HTML 渲染、失败出口、成功出口与请求取词。
func newProjectUITestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.Use(view.FailureMiddleware(view.RenderFailure))
	router.Use(view.UseSuccess(view.NewSuccessWriter()))
	router.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "project", err) }))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	return router
}
