package shell

// back_test.go — 回跳地址构造（back.go）的判据。
//
// 守四件事，每一件出错时都是静默的：
//
//  1. **白名单只透传列出的键** —— 透传过宽时，攻击者能在表单 action 的 query 里塞任意参数，
//     回跳 URL 就变成他可控的（这条与 notice.go 的读侧判定是同一类边界）；
//  2. **路径必须是站内相对路径** —— `//evil.example.com` 这类协议相对地址会让管理员被送出站；
//  3. **值要做形状收敛** —— 超长 / 含控制字符的值丢弃而不是原样拼进 Location；
//  4. **产物稳定** —— 键有序，同一个上下文拼出的 URL 逐字相同（否则「拼岔了」这类
//     diff 会淹没在参数顺序里）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func backCtx(t *testing.T, target string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, target, nil)
	return c
}

// TestBackPathOnlyWhitelistedKeys 只有列出的键会被透传。
func TestBackPathOnlyWhitelistedKeys(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1&status=enabled&evil=1&keyword=k")
	got := BackPath(c, "/admin/coupons", "project", "status")
	want := "/admin/coupons?project=p1&status=enabled"
	if got != want {
		t.Fatalf("期望 %q，实际 %q", want, got)
	}
	if strings.Contains(got, "evil") {
		t.Fatalf("未列出的键被透传：%q", got)
	}
}

// TestBackPathDropsEmptyValues 空值不拼进 URL（避免产出 `?status=&page=` 这种噪音）。
func TestBackPathDropsEmptyValues(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1&status=&keyword=%20%20")
	got := BackPath(c, "/admin/coupons", "project", "status", "keyword")
	if got != "/admin/coupons?project=p1" {
		t.Fatalf("期望只剩 project，实际 %q", got)
	}
}

// TestBackPathNoParams 没有任何参数时保持路径原样（不带问号）。
func TestBackPathNoParams(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create")
	if got := BackPath(c, "/admin/coupons", "project"); got != "/admin/coupons" {
		t.Fatalf("期望 %q，实际 %q", "/admin/coupons", got)
	}
}

// TestBackPathEscapesValues 值里的分隔符必须转义（否则 `a&b=c` 会裂成两个参数）。
func TestBackPathEscapesValues(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?keyword="+`a%26b%3Dc`)
	got := BackPath(c, "/admin/coupons", "keyword")
	if got != "/admin/coupons?keyword=a%26b%3Dc" {
		t.Fatalf("期望转义后的值，实际 %q", got)
	}
}

// TestBackPathRejectsUnsafePath 非站内相对路径一律回控制面首页（防开放重定向）。
func TestBackPathRejectsUnsafePath(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1")
	for _, bad := range []string{
		"//evil.example.com/x",
		"https://evil.example.com",
		"admin/coupons",          // 不以 / 开头：浏览器会按当前路径拼接
		"/admin/x\\..\\evil",     // 反斜杠
		"/admin/x\nLocation: /y", // 控制字符
		"/" + strings.Repeat("a", langRedirectMaxBytes+1),
	} {
		if got := BackPath(c, bad, "project"); got != adminHomePath {
			t.Fatalf("路径 %q 应当被拒绝并回 %q，实际 %q", bad, adminHomePath, got)
		}
	}
}

// TestBackPathDropsOverlongValue 超长值丢弃（其余参数照常）。
func TestBackPathDropsOverlongValue(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1&keyword="+strings.Repeat("k", backValueMaxBytes+1))
	got := BackPath(c, "/admin/coupons", "project", "keyword")
	if got != "/admin/coupons?project=p1" {
		t.Fatalf("超长 keyword 应当被丢弃，实际 %q", got)
	}
}

// TestBackPathDropsControlChars 含控制字符的值丢弃。
func TestBackPathDropsControlChars(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?keyword=a%0Ab&project=p1")
	got := BackPath(c, "/admin/coupons", "project", "keyword")
	if got != "/admin/coupons?project=p1" {
		t.Fatalf("含控制字符的 keyword 应当被丢弃，实际 %q", got)
	}
}

// TestBackPathStableOrder 产物稳定：参数顺序不影响结果（url.Values.Encode 按键排序）。
func TestBackPathStableOrder(t *testing.T) {
	a := backCtx(t, "/admin/coupons/create?status=enabled&project=p1&page=2")
	b := backCtx(t, "/admin/coupons/create?page=2&project=p1&status=enabled")
	ka := BackPath(a, "/admin/coupons", "project", "status", "page")
	kb := BackPath(b, "/admin/coupons", "project", "status", "page")
	if ka != kb {
		t.Fatalf("同一上下文拼出不同 URL：%q vs %q", ka, kb)
	}
	if ka != "/admin/coupons?page=2&project=p1&status=enabled" {
		t.Fatalf("期望按键排序，实际 %q", ka)
	}
}

// TestWithParams 显式参数拼装：空值丢弃，值转义。
func TestWithParams(t *testing.T) {
	cases := []struct {
		path   string
		params map[string]string
		want   string
	}{
		{"/admin/orders", nil, "/admin/orders"},
		{"/admin/orders", map[string]string{"project": ""}, "/admin/orders"},
		{"/admin/orders", map[string]string{"project": "p1"}, "/admin/orders?project=p1"},
		{"/admin/orders", map[string]string{"b": "2", "a": "1"}, "/admin/orders?a=1&b=2"},
		{"/admin/orders", map[string]string{"q": "a b"}, "/admin/orders?q=a+b"},
	}
	for _, c := range cases {
		if got := WithParams(c.path, c.params); got != c.want {
			t.Fatalf("WithParams(%q, %v)=%q，want %q", c.path, c.params, got, c.want)
		}
	}
}
