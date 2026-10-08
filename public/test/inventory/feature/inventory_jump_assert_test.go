package feature

// inventory_jump_assert_test.go — 写动作提示页的共用断言。
//
// 本批把「302 + ?err= / ?ok= / ?done= 回列表页」换成 shell.RenderJump 渲染整页提示
//（对应 ThinkPHP 的 success() / error()），各用例的旧断言（302 + Location）随之改成断言
// 提示页：HTTP 200 + data-jump-state 标记 + 渲染到布局尾部 + 指定文案。
//
// 三条一起断言，缺一条就会漏掉一类缺陷：
//   · 状态码 200 —— 仍是 302 说明还走在旧通道上；
//   · data-jump-state —— 提示页真的渲染了（不是别的页面）；
//   · </html> —— 模板在某一行中断时 HTTP 仍 200、后半截整块消失（见 templates/CLAUDE.md）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// assertInventoryJump 断言一次写动作的响应是整页提示。
func assertInventoryJump(t *testing.T, rec *httptest.ResponseRecorder, state string, wants ...string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("提示页应为 200（不再是 302 + ?err=），实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-jump-state="`+state+`"`) {
		t.Fatalf("提示页缺少 data-jump-state=%q：%s", state, body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatal("提示页未渲染到布局尾部（模板在某一行中断）")
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("提示页缺少 %q", want)
		}
	}
}
