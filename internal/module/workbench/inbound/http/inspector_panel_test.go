package workbenchhttp

// inspector_panel_test.go — 检查器面板服务端渲染（HTMX 化打样，docs/09 §3）。
//
// 验证：schema → 表单 HTML 由服务端渲染，字段带 data-wb-path/data-wb-kind，
// 当前值回填；无选中节点时输出空态提示。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// newInspectorRouter 装配仅含检查器端点的测试路由。
func newInspectorRouter(t *testing.T) *gin.Engine {
	t.Helper()
	handle := &Handle{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.POST("/workbench/inspector", handle.InspectorPanel)
	return router
}

// TestInspectorPanelRendersFields 渲染 heading 组件字段并回填当前值。
func TestInspectorPanelRendersFields(t *testing.T) {
	router := newInspectorRouter(t)
	doc := `{"settings":{},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好","tag":"h2","color":"#ff0000","advanced":{"opacity":80,"radius":{"topLeft":"8px"}}}}]}`
	form := url.Values{"nodeId": {"h1"}, "document": {doc}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"内容",
		`data-wb-path="text"`,
		">你好<",
		`data-wb-path="tag"`,
		`<option value="h2" selected>`,
		`data-wb-path="advanced.opacity"`,
		`value="80"`,
		"wb-inspector-group",
		// 增强字段：服务端只输出定位占位，客户端用既有控件函数填充。
		`data-wb-slot="color"`,
		`data-wb-enhance="corners"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %q\n%s", want, body)
		}
	}
}

// TestInspectorPanelEmptyState 无选中节点时输出空态提示。
func TestInspectorPanelEmptyState(t *testing.T) {
	router := newInspectorRouter(t)
	form := url.Values{"nodeId": {"missing"}, "document": {`{"settings":{},"root":[]}`}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "选择组件") {
		t.Errorf("应输出空态提示，实际: %s", recorder.Body.String())
	}
}

// fetchInspectorPanel 按 tab 请求检查器片段（tab 为空则不过滤）。
func fetchInspectorPanel(t *testing.T, router *gin.Engine, tab string) string {
	t.Helper()
	doc := `{"settings":{},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好","tag":"h2","color":"#ff0000","advanced":{"opacity":80}}}]}`
	form := url.Values{"nodeId": {"h1"}, "document": {doc}, "tab": {tab}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	return recorder.Body.String()
}

// TestInspectorPanelTabFiltering 页签由服务端过滤分组：
// content 页签只出内容字段，style 页签只出样式/高级字段。
func TestInspectorPanelTabFiltering(t *testing.T) {
	router := newInspectorRouter(t)

	content := fetchInspectorPanel(t, router, "content")
	if !strings.Contains(content, `data-wb-path="text"`) {
		t.Errorf("content 页签缺少内容字段\n%s", content)
	}
	if strings.Contains(content, `data-wb-path="advanced.opacity"`) {
		t.Errorf("content 页签不应包含样式字段\n%s", content)
	}

	style := fetchInspectorPanel(t, router, "style")
	if !strings.Contains(style, `data-wb-path="advanced.opacity"`) {
		t.Errorf("style 页签缺少样式字段\n%s", style)
	}
	if strings.Contains(style, `data-wb-path="text"`) {
		t.Errorf("style 页签不应包含内容字段\n%s", style)
	}

	// tab 为空：向后兼容，渲染全部。
	all := fetchInspectorPanel(t, router, "")
	if !strings.Contains(all, `data-wb-path="text"`) || !strings.Contains(all, `data-wb-path="advanced.opacity"`) {
		t.Errorf("空 tab 应渲染全部字段\n%s", all)
	}
}

// TestInspectorPanelRichTextSlot 富文本内容字段（ct:"richtext"）输出 richtext 增强槽：
// 服务端只给定位占位，客户端 richTextField（Trix）就地填充；
// core.text 的 mode=plaintext 由前端 isPlainTextMode 回退多行输入。
func TestInspectorPanelRichTextSlot(t *testing.T) {
	router := newInspectorRouter(t)
	doc := `{"settings":{},"root":[{"id":"cd1","type":"core.card","props":{"title":"卡片","text":"<p>正文</p>"}}]}`
	form := url.Values{"nodeId": {"cd1"}, "document": {doc}, "tab": {"content"}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`data-wb-slot="text"`,
		`data-wb-enhance="richtext"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %q\n%s", want, body)
		}
	}
	// 富文本字段不再由服务端直出输入框（避免两套编辑形态并存）。
	if strings.Contains(body, `data-wb-path="text"`) {
		t.Errorf("富文本字段不应直出输入框\n%s", body)
	}
}

// TestInspectorPanelBoxSpacingSlot container 的内外距输出 boxspacing 增强槽
// （客户端按「一行四向 + 默认联动」编辑，数据仍是 CSS 简写）。
func TestInspectorPanelBoxSpacingSlot(t *testing.T) {
	router := newInspectorRouter(t)
	doc := `{"settings":{},"root":[{"id":"c1","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{}},"box":{"padding":{"desktop":"10px 20px"},"margin":{"desktop":"auto"}}}}]}`
	// box.padding/margin 的 sec=layout → 属于「样式」页签。
	form := url.Values{"nodeId": {"c1"}, "document": {doc}, "tab": {"style"}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`data-wb-slot="box.padding"`,
		`data-wb-enhance="boxspacing"`,
		`data-wb-slot="box.margin"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %q\n%s", want, body)
		}
	}
}
