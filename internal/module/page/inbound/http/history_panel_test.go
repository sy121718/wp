package pagehttp

// history_panel_test.go — 修订历史列表片段的服务端渲染（HTMX 化，docs/09 §3）。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// TestHistoryPanelEmptyState 无 page 契约/无修订时输出空态提示。
func TestHistoryPanelEmptyState(t *testing.T) {
	handle := NewPagesAdminHandle(nil, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.POST("/workbench/history", handle.HistoryPanel)

	form := url.Values{"pageId": {"page-1"}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/history", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("状态码 %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "还没有修订历史") {
		t.Errorf("应输出空态提示，实际: %s", recorder.Body.String())
	}
}
