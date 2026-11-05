package adminhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func adminJumpCtx(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "/admin/roles/bulk-delete?"+rawQuery, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestAdminListQueryOnlyWhitelistedKeys(t *testing.T) {
	c := adminJumpCtx(t, "keyword=%E8%A7%92%E8%89%B2&page=2&evil=payload&limit=20")
	got := adminListQuery(c, adminRolesBackKeys...)
	for _, want := range []string{"keyword=", "page=2", "limit=20"} {
		if !strings.Contains(got, want) {
			t.Errorf("白名单键 %q 应被透传，got %q", want, got)
		}
	}
	if strings.Contains(got, "evil") {
		t.Errorf("白名单之外的键不许透传，got %q", got)
	}
	if got := adminListQuery(c); got != "" {
		t.Errorf("未给键时不应透传任何参数，got %q", got)
	}
}
