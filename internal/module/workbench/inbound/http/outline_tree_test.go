package workbenchhttp

import (
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

const outlineDocument = `{"settings":{},"root":[{"id":"sec1","type":"core.container","props":{},"children":[{"id":"h1","type":"core.heading","props":{},"name":"主标题"},{"id":"t1","type":"core.text","props":{}}]}]}`

func newOutlineRouter(t *testing.T) *gin.Engine {
	t.Helper()
	handle := &Handle{}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.POST("/workbench/outline", adminui.BareSlot(OutlineTreeTemplate), handle.OutlineTree)
	return router
}

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

func TestOutlineTreeRendersNestedNodes(t *testing.T) {
	body := fetchOutline(t, newOutlineRouter(t), "h1", "")
	for _, want := range []string{

		`<div class="wb-node is-selected" role="treeitem" tabindex="0" data-id="h1" data-type="core.heading" draggable="true">`,
		`data-id="sec1" data-type="core.container"`,
		`data-wb-op="up"`,
		`data-wb-op="del"`,
		`class="wb-caret"`,
		`data-named="1">主标题<`,
		`data-named="">text<`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("结构树缺少 %q\n%s", want, body)
		}
	}
}

func TestOutlineTreeFilterKeepsAncestors(t *testing.T) {
	body := fetchOutline(t, newOutlineRouter(t), "", "主标题")
	if !strings.Contains(body, `data-id="sec1"`) || !strings.Contains(body, `data-id="h1"`) {
		t.Errorf("命中链路应保留祖先与自身\n%s", body)
	}
	if strings.Contains(body, `data-id="t1"`) {
		t.Errorf("未命中节点应被过滤\n%s", body)
	}
}
