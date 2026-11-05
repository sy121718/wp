package workbenchhttp

import (
	pageview "go_wp/pkg/view"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func newInspectorRouter(t *testing.T) *gin.Engine {
	t.Helper()
	handle := &Handle{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.POST("/workbench/inspector", pageview.Slot(InspectorPanelTemplate, adminui.RenderBare), handle.InspectorPanel)
	return router
}

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

		`data-wb-slot="color"`,
		`data-wb-enhance="corners"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("面板缺少 %q\n%s", want, body)
		}
	}
}

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

	all := fetchInspectorPanel(t, router, "")
	if !strings.Contains(all, `data-wb-path="text"`) || !strings.Contains(all, `data-wb-path="advanced.opacity"`) {
		t.Errorf("空 tab 应渲染全部字段\n%s", all)
	}
}

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

	if strings.Contains(body, `data-wb-path="text"`) {
		t.Errorf("富文本字段不应直出输入框\n%s", body)
	}
}

func TestInspectorPanelBoxSpacingSlot(t *testing.T) {
	router := newInspectorRouter(t)
	doc := `{"settings":{},"root":[{"id":"c1","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{}},"box":{"padding":{"desktop":"10px 20px"},"margin":{"desktop":"auto"}}}}]}`

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
