package userhttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// customerWriteEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场。
func customerWriteEngine(fake *fakeCustomerAdmin) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	h := NewCustomerHandle(fake)
	r.POST("/api/customer/status", pageview.HTMLFragment, h.SetCustomerStatus)
	r.POST("/api/customer/unlock", pageview.HTMLFragment, h.UnlockCustomer)
	r.POST("/api/customer/bulk-status", pageview.HTMLFragment, h.BulkCustomerStatus)
	r.POST("/api/customer/bulk-unlock", pageview.HTMLFragment, h.BulkCustomerUnlock)
	return r
}

func customerWriteResp(t *testing.T, fake *fakeCustomerAdmin, path string, form url.Values) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	customerWriteEngine(fake).ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("界面响应必须是 HTML，实际 %q，正文：%s", rec.Header().Get("Content-Type"), body)
	}
	return rec.Code, body
}

func TestCustomerStatusRequiresExplicitTarget(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	for _, form := range []url.Values{
		{"customerId": {"42"}},
		{"customerId": {"42"}, "status": {""}},
		{"customerId": {"0"}, "status": {"0"}},
		{"customerId": {"42"}, "status": {"7"}},
	} {
		code, body := customerWriteResp(t, fake, "/api/customer/status", form)
		if code != http.StatusBadRequest || fake.statusReq != nil {
			t.Fatalf("缺目标状态却写了库：%d %s", code, body)
		}
		if !strings.Contains(body, "data-ui-error") {
			t.Fatalf("失败应渲染错误片段，实际 %s", body)
		}
	}
}

func TestCustomerStatusWritesAndReturnsFragment(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	code, body := customerWriteResp(t, fake, "/api/customer/status", url.Values{
		"customerId": {"42"}, "status": {"0"},
	})
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if fake.statusReq == nil || fake.statusReq.CustomerID != 42 || fake.statusReq.Status != 0 {
		t.Fatalf("应把客户与目标状态交给 service，实际 %+v", fake.statusReq)
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("成功应回 Jet 结果片段，实际 %s", body)
	}
}

func TestCustomerBulkStatusReturnsEveryOutcome(t *testing.T) {
	fake := &fakeCustomerAdmin{failIDs: map[uint64]bool{7: true}}
	code, body := customerWriteResp(t, fake, "/api/customer/bulk-status", url.Values{
		"ids": {"42", "7", "42", "43"}, "status": {"0"},
	})
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if got := fake.statusIDs; len(got) != 3 || got[0] != 42 || got[1] != 7 || got[2] != 43 {
		t.Fatalf("未逐条去重保序：%v", got)
	}
	if !strings.Contains(body, "已更新 2 个客户") || !strings.Contains(body, "1 个未处理") {
		t.Fatalf("计数/逐项结果失真：%s", body)
	}
	if !strings.Contains(body, "7：") {
		t.Fatalf("失败条目必须带 id 与原因：%s", body)
	}
}

func TestCustomerBulkUnlockSeparatesChangedAndNoop(t *testing.T) {
	fake := &fakeCustomerAdmin{unlockByID: map[uint64]*userdto.CustomerUnlockResp{
		42: {CustomerID: 42, Unlocked: true}, 43: {CustomerID: 43, Cleared: true}, 44: {CustomerID: 44},
	}}
	code, body := customerWriteResp(t, fake, "/api/customer/bulk-unlock", url.Values{"ids": {"42", "43", "44"}})
	if code != http.StatusOK {
		t.Fatalf("status=%d body=%s", code, body)
	}
	if !strings.Contains(body, "已解除锁定 1 个") || !strings.Contains(body, "清零失败计数 1 个") ||
		!strings.Contains(body, "1 个本来就未锁定") || !strings.Contains(body, "0 个未处理") {
		t.Fatalf("解锁结果三态失真：%s", body)
	}
}

func TestCustomerBulkRejectsEmptyAndOverLimit(t *testing.T) {
	fake := &fakeCustomerAdmin{}
	code, body := customerWriteResp(t, fake, "/api/customer/bulk-status", url.Values{"status": {"0"}})
	if code != http.StatusBadRequest || fake.statusIDs != nil {
		t.Fatalf("空勾选应 400 且不写库：%d %s", code, body)
	}
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染错误片段：%s", body)
	}
}
