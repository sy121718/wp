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
			"title":      "测试页",
			"pageId":     "p-1",
			"isBlock":    false,
			"isTemplate": false,
			"draftPath":  "/about",
			"version":    3,
			"document":   `{"settings":{},"root":[]}`,
			"meta":       `{"pageId":"p-1","draftPath":"/about","version":3}`,
			"schemas":    `{"core.heading":[{"key":"text","kind":"text","section":"content"}]}`,
			// jsVer 由真实 handler 注入（dashboard_handle.go）；模板 head/body 均引用它，
			// 缺失会让 Jet 在第一个 jsVer 表达式处运行时报错并截断输出。
			"jsVer": "test-1",
			// 取词函数：模板顶部 `{{tr := .["t"]}}` 从 data 取它（真实路径由 shell.Prepare
			// 注入）。**缺这个键时模板里所有 tr(...) 都输出空** —— class / id 结构仍在，
			// 中文文案却整片消失，于是「外壳缺少区块 组件」这类断言会把「测试没给取词函数」
			// 误报成「模板缺了区块」。与其它模板测试同口径走 TranslateFunc。
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
		// 外壳骨架（2026-09 重构后）：左侧图标条 wb-rail-* + 抽屉面板 wb-panel-*，
		// 检查器为 inspector-panel，状态条为 wb-status；旧的 topbar / inspector /
		// bottombar 三块已并入该布局。
		"wb-rail-library", "wb-rail-navigator", "wb-palette", "组件", "wb-canvas", "wb-canvas-frame",
		"wb-navigator", "inspector-panel", "wb-status",
		"wb-save-draft", "wb-publish", "wb-device-desktop",
		`id="wb-bootstrap"`, `{"settings":{},"root":[]}`,
		`id="wb-meta"`, `{"pageId":"p-1","draftPath":"/about","version":3}`,
		`id="wb-schemas"`, `"core.heading"`,
		// 前端已拆分为 ES modules（docs/09 §3）：入口 index.js + core + methods/*。
		"/static/js/workbench/index.js", "存草稿", "editor=1",
		// 富文本编辑器为 Trix，本地 vendor 资源（不再走 CDN）。
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
