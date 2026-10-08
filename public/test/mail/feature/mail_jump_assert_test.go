package feature

// mail_jump_assert_test.go — 写动作结论走**整页提示**（shell.RenderJump）的断言助手。
//
// 取代原先对「302 + Location 上的 ?err= / ?ok= / ?done=」的断言：那条通道已随
// 「结论走响应体」整批删除（见 internal/module/mail/inbound/http/mail_jump.go）。
// 现在写动作回 **HTTP 200 + 提示页**：`data-jump-state="ok|err"` 标出成功 / 失败，
// 结论文案直接在响应体里，回跳地址在 meta refresh / 链接上。

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// mailAssertJump 断言响应是提示页，并检查状态与结论文案。
func mailAssertJump(t *testing.T, rec *httptest.ResponseRecorder, wantOK bool, wantText string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("提示页应回 200，实际 %d，正文前 300 字：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	body := rec.Body.String()
	state := `data-jump-state="ok"`
	label := "成功"
	if !wantOK {
		state = `data-jump-state="err"`
		label = "失败"
	}
	if !strings.Contains(body, state) {
		t.Fatalf("提示页应是%s态（缺 %s）；正文前 600 字：\n%s", label, state, firstN(body, 600))
	}
	if wantText != "" && !strings.Contains(body, wantText) {
		t.Fatalf("提示页应含文案 %q；正文前 600 字：\n%s", wantText, firstN(body, 600))
	}
}

// mailJumpBack 从提示页里取回跳地址（meta refresh 的 url= 或「立即前往」链接的 href）。
func mailJumpBack(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	body := rec.Body.String()
	if m := regexp.MustCompile(`url=([^">]+)`).FindStringSubmatch(body); m != nil {
		return m[1]
	}
	if m := regexp.MustCompile(`href="(/admin/[^"]*)"`).FindStringSubmatch(body); m != nil {
		return m[1]
	}
	t.Fatalf("提示页里找不到回跳地址；正文前 600 字：\n%s", firstN(body, 600))
	return ""
}
