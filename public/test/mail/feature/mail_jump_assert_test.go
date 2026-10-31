package feature

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

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
