package workbenchhttp

// outline_tree_test.go — 结构树服务端渲染（HTMX 化，docs/09 §3）。
//
// 验证：嵌套树 HTML、选中态、拖拽所需的 data-type/data-id、节点操作按钮、
// 过滤只保留命中链路、未命名节点回退为组件类型名。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// outlineDocument 两级嵌套文档：container > heading + text。
const outlineDocument = `{"settings":{},"root":[{"id":"sec1","type":"core.container","props":{},"children":[{"id":"h1","type":"core.heading","props":{},"name":"主标题"},{"id":"t1","type":"core.text","props":{}}]}]}`

// newOutlineRouter 装配仅含结构树端点的测试路由。
func newOutlineRouter(t *testing.T) *gin.Engine {
	t.Helper()
	handle := &Handle{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.POST("/workbench/outline", handle.OutlineTree)
	return router
}

// fetchOutline 请求结构树片段。
func fetchOutline(t *testing.T, router *gin.Engine, selectedID, filter string) string {
	t.Helper()
	form := url.Values{"document": {outlineDocument}, "selectedId": {selectedID}, "filter": {filter}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/outline", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	return recorder.Body.String()
}

// TestOutlineTreeRendersNestedNodes 嵌套结构与节点操作按钮渲染完整。
func TestOutlineTreeRendersNestedNodes(t *testing.T) {
	body := fetchOutline(t, newOutlineRouter(t), "h1", "")
	for _, want := range []string{
		// role/tabindex：键盘可达（a11y P1 收尾）
		`<div class="wb-node is-selected" role="treeitem" tabindex="0" data-id="h1" data-type="core.heading" draggable="true">`,
		`data-id="sec1" data-type="core.container"`,
		`data-wb-op="up"`,
		`data-wb-op="del"`,
		`class="wb-caret"`,
		`data-named="1">主标题<`, // 用户命名优先
		`data-named="">text<`, // 未命名回退为类型名（客户端换中文）
	} {
		if !strings.Contains(body, want) {
			t.Errorf("结构树缺少 %q\n%s", want, body)
		}
	}
}

// TestOutlineTreeFilterKeepsAncestors 过滤只保留命中链路（含祖先）。
func TestOutlineTreeFilterKeepsAncestors(t *testing.T) {
	body := fetchOutline(t, newOutlineRouter(t), "", "主标题")
	if !strings.Contains(body, `data-id="sec1"`) || !strings.Contains(body, `data-id="h1"`) {
		t.Errorf("命中链路应保留祖先与自身\n%s", body)
	}
	if strings.Contains(body, `data-id="t1"`) {
		t.Errorf("未命中节点应被过滤\n%s", body)
	}
}
