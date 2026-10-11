package userhttp

// customer_notice_test.go — 批量结论渲染进提示页（shell.RenderJump）的回归。
//
// 取代原先对 ?done= 读侧受控出口（customerPageDone / customerBulkNoticeCandidates）的回归：
// 写结论不再经查询参数回带，手拼 ?done= 也无从注入 —— 这里钉住的是**写侧真实产出的每一种
// 句子都被渲染进响应体**。此前那条通道最容易出的错就是「成功回执静默消失」
// （写侧改了措辞、读侧再也认不出，不报错、日志里也没有），现在文案走响应体，这条风险随之消失。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	userdto "go_wp/internal/module/user/dto"
)

// TestCustomerBulkJumpRendersEveryWriterShape 批量动作的每一种分支都渲染进提示页。
func TestCustomerBulkJumpRendersEveryWriterShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		path   string
		form   url.Values
		fake   *fakeCustomerAdmin
		states []string
	}{
		{
			name:   "批量停用（部分未处理）",
			path:   "/api/customer/bulk-status",
			form:   url.Values{"ids": {"42", "7", "43"}, "toStatus": {"0"}},
			fake:   &fakeCustomerAdmin{failIDs: map[uint64]bool{7: true}},
			states: []string{`"code":200`, `"updated":2`, `"skipped":1`},
		},
		{
			name:   "批量启用",
			path:   "/api/customer/bulk-status",
			form:   url.Values{"ids": {"42", "43"}, "toStatus": {"1"}},
			fake:   &fakeCustomerAdmin{},
			states: []string{`"code":200`, `"updated":2`},
		},
		{
			name: "批量解锁（解开 / 本来就没事）",
			path: "/api/customer/bulk-unlock",
			form: url.Values{"ids": {"42", "43"}},
			fake: &fakeCustomerAdmin{unlockByID: map[uint64]*userdto.CustomerUnlockResp{
				42: {CustomerID: 42, Unlocked: true},
				43: {CustomerID: 43, Unlocked: false},
			}},
			states: []string{`"code":200`, "已解除锁定 1 个", "1 个本来就未锁定"},
		},
		{
			name:   "批量一条都没勾",
			path:   "/api/customer/bulk-status",
			form:   url.Values{"toStatus": {"0"}},
			fake:   &fakeCustomerAdmin{},
			states: []string{`"code":400`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewCustomerPageHandle(tc.fake, fakeOrderSummaryReader{}, fakeProjects{})
			engine := newCustomerTestEngine(h)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(customerJSONForm(t, tc.form)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK && tc.name != "批量一条都没勾" {
				t.Fatalf("应渲染提示页（200），实际 %d", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tc.states {
				if !strings.Contains(body, want) {
					t.Errorf("提示页应含 %q", want)
				}
			}
		})
	}
}
