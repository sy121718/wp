package feature

// inventory_jump_assert_test.go — 写动作出口的共用断言。
//
// 历史：这里曾断言「整页提示」（data-jump-state + </html>）。提示页已删除 —— 结论不再占一页：
//   · 成功 = 200 + JSON（{code:200,message,data}），**不跳转、不给 URL**：导航由前端按模板的
//     data-write-nav 决定（无值 = 重载当前页）；
//   · 失败 = 4xx / 5xx + JSON，htmx 对 4xx 不 swap，页面弹 toast。
//
// 所以本助手只认这两条形态，state 参数仍按 "ok" / "err" 传（调用点不改）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// assertInventoryJump 断言一次写动作的响应形态。
//
//   - state = "ok"：200 + JSON，且不带 Location / HX-Redirect（后端不给跳转地址）；
//   - state = "err"：4xx/5xx + JSON，wants 逐条匹配响应体（即 message 的文案）。
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
