package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func TestWorkbenchPageShell(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/workbench", func(c *gin.Context) {
		c.HTML(http.StatusOK, "workbench/layout", gin.H{
			"title":      "测试页",
			"pageId":     "p-1",
			"isBlock":    false,
			"isTemplate": false,
			"draftPath":  "/about",
			"version":    3,
			"document":   `{"settings":{},"root":[]}`,
			"meta":       `{"pageId":"p-1","draftPath":"/about","version":3}`,
			"schemas":    `{"core.heading":[{"key":"text","kind":"text","section":"content"}]}`,

			"jsVer": "test-1",

			"t": templates.TranslateFunc("zh-CN"),
		})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/workbench", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("工作台外壳渲染失败: %d %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, fragment := range []string{

		"wb-panel-library", "wb-navigator-toggle", "wb-palette", "组件", "wb-canvas", "wb-canvas-frame",
		"wb-navigator", "inspector-panel", "wb-status",
		"wb-save-draft", "wb-publish", "wb-device-desktop",
		`id="wb-bootstrap"`, `{"settings":{},"root":[]}`,
		`id="wb-meta"`, `{"pageId":"p-1","draftPath":"/about","version":3}`,
		`id="wb-schemas"`, `"core.heading"`,

		"/static/js/workbench/index.js", "存草稿", "editor=1",

		"/static/vendor/trix/trix.umd.js", "/static/vendor/trix/trix.css",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("外壳缺少区块 %s", fragment)
		}
	}
	if strings.Contains(strings.ToLower(body), "tinymce") {
		t.Fatalf("外壳仍引用 TinyMCE: %s", body)
	}
	if strings.Contains(body, "&quot;") {
		t.Fatalf("JSON 数据岛被 HTML 转义: %s", body)
	}
}
