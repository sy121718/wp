package pagehttp

import (
	"go_wp/internal/adminui"
	"go_wp/internal/text"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func TestHistoryPanelEmptyState(t *testing.T) {
	handle := NewPagesAdminHandle(nil, nil, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	router.POST("/workbench/history", adminui.BareSlot(PageHistoryListTemplate), handle.HistoryPanel)

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
