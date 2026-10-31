package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func assertInventoryJump(t *testing.T, rec *httptest.ResponseRecorder, state string, wants ...string) {
	t.Helper()
	if state == "ok" {
		if rec.Code != http.StatusOK {
			t.Fatalf("成功应为 200 + JSON，实际 %d：%s", rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Fatalf("成功不该给 Location（导航在前端），实际 %q", loc)
		}
		if nav := rec.Header().Get("HX-Redirect"); nav != "" {
			t.Fatalf("成功不该给 HX-Redirect（导航在前端），实际 %q", nav)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Fatalf("成功响应 Content-Type = %q，want application/json", ct)
		}
		return
	}
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("失败应为 4xx/5xx + JSON，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败响应 Content-Type = %q，want application/json", ct)
	}
	body := rec.Body.String()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("响应缺少 %q：%s", want, body)
		}
	}
}
