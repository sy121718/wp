package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// assertInventoryJump 核对库存写端点的统一出口：成功是 Jet 成功片段，
// 失败是 Jet 错误片段；两条都不由后端控制导航。
func assertInventoryJump(t *testing.T, rec *httptest.ResponseRecorder, state string, wants ...string) {
	t.Helper()
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("写响应不得给 Location（导航在前端），实际 %q", loc)
	}
	if nav := rec.Header().Get("HX-Redirect"); nav != "" {
		t.Fatalf("写响应不得给 HX-Redirect（导航在前端），实际 %q", nav)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("写响应 Content-Type = %q，want text/html", ct)
	}
	body := rec.Body.String()
	if state == "ok" {
		if rec.Code != http.StatusOK {
			t.Fatalf("成功应为 200 + HTML 片段，实际 %d：%s", rec.Code, body)
		}
		if !strings.Contains(body, `role="status"`) {
			t.Fatalf("成功应回 Jet 成功片段：%s", body)
		}
		return
	}
	if rec.Code < 400 || rec.Code >= 600 {
		t.Fatalf("失败应为 4xx/5xx + HTML 片段，实际 %d：%s", rec.Code, body)
	}
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("失败应渲染错误片段：%s", body)
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("响应缺少 %q：%s", want, body)
		}
	}
}
