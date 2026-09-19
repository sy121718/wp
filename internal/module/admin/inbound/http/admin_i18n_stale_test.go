package adminhttp

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeI18nPageMarker 记录 MarkStaleForI18n 调用次数并可控返回错误。
type fakeI18nPageMarker struct {
	calls int
	err   error
}

func (f *fakeI18nPageMarker) MarkStaleForI18n(ctx context.Context) error {
	f.calls++
	return f.err
}

// TestAdminI18nMarkStaleCallsPageMarker 词条变更后必须调用页面失效端口。
//
// 这是"改了词条站点不更新"的唯一防线：sys_i18n 的词条在构建期被取词并烘进产物字节，
// 不标 stale 就永远输出旧文案，而且没有任何报错 —— 属于静默失效。
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

// TestAdminI18nMarkStaleSwallowsError 标记失败不得改动响应。
//
// 词条此刻已经写进库了：此时回报失败会让运营以为没保存而反复重试；
// 而站点停在旧文案是可见的降级（下次编辑或发布会自然覆盖）。
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

// TestAdminI18nMarkStaleWithoutMarker 未注入端口时静默返回。
//
// 装配漏接的表现是"改了词条站点不更新"，由 wiring 的端口清单在启动自检里兜底；
// 这里只要求它不 panic、不阻断请求。
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
