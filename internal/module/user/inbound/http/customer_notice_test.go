package userhttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	userdto "go_wp/internal/module/user/dto"
)

// 客户批量动作已统一为「标准表单 + Jet 片段」：成功换结果区、失败换错误区，
// 都不整页跳转，也不再有 JSON 回执。
func TestCustomerBulkWritesReturnHTMLFragments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		form   url.Values
		fake   *fakeCustomerAdmin
		status int
		states []string
	}{
		{
			name:   "批量停用（部分未处理）",
			path:   "/api/customer/bulk-status",
			form:   url.Values{"ids": {"42", "7", "43"}, "status": {"0"}},
			fake:   &fakeCustomerAdmin{failIDs: map[uint64]bool{7: true}},
			status: http.StatusOK,
			states: []string{"已更新 2 个客户", "1 个未处理", "7："},
		},
		{
			name:   "批量启用",
			path:   "/api/customer/bulk-status",
			form:   url.Values{"ids": {"42", "43"}, "status": {"1"}},
			fake:   &fakeCustomerAdmin{},
			status: http.StatusOK,
			states: []string{"已更新 2 个客户"},
		},
		{
			name: "批量解锁（解开 / 本来就没事）",
			path: "/api/customer/bulk-unlock",
			form: url.Values{"ids": {"42", "43"}},
			fake: &fakeCustomerAdmin{unlockByID: map[uint64]*userdto.CustomerUnlockResp{
				42: {CustomerID: 42, Unlocked: true},
				43: {CustomerID: 43},
			}},
			status: http.StatusOK,
			states: []string{"已解除锁定 1 个", "1 个本来就未锁定"},
		},
		{
			name:   "批量一条都没勾",
			path:   "/api/customer/bulk-status",
			form:   url.Values{"status": {"0"}},
			fake:   &fakeCustomerAdmin{},
			status: http.StatusBadRequest,
			states: []string{"data-ui-error"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewCustomerPageHandle(tc.fake, fakeOrderSummaryReader{}, fakeProjects{})
			engine := newCustomerTestEngine(h)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("应 %d，实际 %d：%s", tc.status, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("写响应必须是 HTML 片段，实际 %q", ct)
			}
			body := rec.Body.String()
			for _, want := range tc.states {
				if !strings.Contains(body, want) {
					t.Errorf("结果片段应含 %q，实际 %s", want, body)
				}
			}
		})
	}
}
