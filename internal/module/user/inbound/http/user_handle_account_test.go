package userhttp

// user_handle_account_test.go — 账号中心保存后的**回跳目标**校验。
//
// 这块只有一条规则，但它是安全规则：回跳目标最终要写进 Location 响应头。
// 照抄表单里的值就是开放重定向 —— 站点变成任意目的地的跳板，而页面看起来一切正常。
// 所以这里逐个钉住被拒绝的形状。

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// postFormContext 造一个带 POST 表单的 gin context。
func postFormContext(t *testing.T, form url.Values) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	body := form.Encode()
	c.Request = httptest.NewRequest("POST", "/user/account/profile", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c
}

func TestAccountNextTarget(t *testing.T) {
	tests := []struct {
		name string
		next string
		want string
	}{
		{"站内相对路径", "/my-account", "/my-account"},
		{"带查询串", "/my-account?tab=profile", "/my-account?tab=profile"},
		{"两端空白被裁掉", "  /my-account  ", "/my-account"},
		{"空值", "", ""},
		{"协议相对 URL 被拒", "//evil.example", ""},
		{"绝对 URL 被拒", "https://evil.example/x", ""},
		{"裸主机名被拒", "evil.example", ""},
		{"反斜杠构造被拒", "/\\evil.example", ""},
		{"换行注入被拒", "/x\nSet-Cookie: a=b", ""},
		{"回车注入被拒", "/x\r\nX: y", ""},
		{"超长被拒", "/" + strings.Repeat("a", 600), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := postFormContext(t, url.Values{"next": {tt.next}})
			if got := accountNextTarget(c); got != tt.want {
				t.Fatalf("accountNextTarget(%q)=%q, want=%q", tt.next, got, tt.want)
			}
		})
	}
}

// TestRedirectAfterSave 没配回跳时不接管响应（保持内置账号页的原行为）。
func TestRedirectAfterSave(t *testing.T) {
	c := postFormContext(t, url.Values{})
	if redirectAfterSave(c) {
		t.Fatal("没有 next 时不该回跳（内置账号页要继续渲染）")
	}
	c2 := postFormContext(t, url.Values{"next": {"/my-account"}})
	if !redirectAfterSave(c2) {
		t.Fatal("带合法 next 时应回跳")
	}
	if c2.Writer.Status() != 302 {
		t.Fatalf("回跳应为 302，实际 %d", c2.Writer.Status())
	}
	if loc := c2.Writer.Header().Get("Location"); loc != "/my-account" {
		t.Fatalf("Location 应为 /my-account，实际 %q", loc)
	}
}
