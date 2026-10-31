package adminhttp

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeI18nPageMarker struct {
	calls int
	err   error
}

func (f *fakeI18nPageMarker) MarkStaleForI18n(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestAdminI18nMarkStaleCallsPageMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	marker := &fakeI18nPageMarker{}
	h := NewAdminI18nEntryHandle()
	h.SetPageMarker(marker)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/admin/i18n/save", nil)

	h.markI18nStale(c)

	if marker.calls != 1 {
		t.Fatalf("词条变更应调用 MarkStaleForI18n 一次，实际 %d 次", marker.calls)
	}
}

func TestAdminI18nMarkStaleSwallowsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	marker := &fakeI18nPageMarker{err: errors.New("boom")}
	h := NewAdminI18nEntryHandle()
	h.SetPageMarker(marker)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/admin/i18n/save", nil)

	h.markI18nStale(c)

	if marker.calls != 1 {
		t.Fatalf("应尝试调用一次，实际 %d", marker.calls)
	}
	if rec.Code != 200 {
		t.Fatalf("标记失败不应改动响应状态（默认 200），实际 %d", rec.Code)
	}
}

func TestAdminI18nMarkStaleWithoutMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAdminI18nEntryHandle()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/admin/i18n/save", nil)

	h.markI18nStale(c)

	if rec.Code != 200 {
		t.Fatalf("未注入端口时不应改动响应，实际 %d", rec.Code)
	}
}
