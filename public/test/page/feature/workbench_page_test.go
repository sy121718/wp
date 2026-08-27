package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// TestWorkbenchPageShell 使用生产 Jet 渲染器验证工作台外壳和 JSON 数据岛。
func TestWorkbenchPageShell(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/workbench", func(c *gin.Context) {
		c.HTML(http.StatusOK, "workbench/layout", gin.H{
			"title":     "测试页",
			"pageId":    "p-1",
			"draftPath": "/about",
			"version":   3,
			"document":  `{"settings":{},"root":[]}`,
			"meta":      `{"pageId":"p-1","draftPath":"/about","version":3}`,
		})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/workbench", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("工作台外壳渲染失败: %d %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, fragment := range []string{
		"wb-topbar", "wb-inspector", "wb-canvas", "wb-navigator", "wb-bottombar",
		`id="wb-bootstrap"`, `{"settings":{},"root":[]}`,
		`id="wb-meta"`, `{"pageId":"p-1","draftPath":"/about","version":3}`,
		"/static/js/workbench.js", "存草稿", "editor=1",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("外壳缺少区块 %s", fragment)
		}
	}
	if strings.Contains(body, "&quot;") {
		t.Fatalf("JSON 数据岛被 HTML 转义: %s", body)
	}
}
