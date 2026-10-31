package adminui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newListQueryContext(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/products?"+rawQuery, nil)
	return c
}

func TestListQueryDropsEmptyAndUnknown(t *testing.T) {
	c := newListQueryContext(t, "project=p1&keyword=&status=&source=x")
	if got := ListQuery(c, "project", "keyword", "status"); got != "project=p1" {
		t.Errorf("ListQuery = %q，want project=p1", got)
	}
}

func TestWithListQuery(t *testing.T) {
	if got := WithListQuery("/admin/products", ""); got != "/admin/products" {
		t.Errorf("空上下文 = %q", got)
	}
	if got := WithListQuery("/admin/products", "project=p1"); got != "/admin/products?project=p1" {
		t.Errorf("首次拼接 = %q", got)
	}
	if got := WithListQuery("/x?a=1", "project=p1"); got != "/x?a=1&project=p1" {
		t.Errorf("追加拼接 = %q", got)
	}
}
