package shell

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/pkg/safepath"

	"github.com/gin-gonic/gin"
)

func backCtx(t *testing.T, target string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, target, nil)
	return c
}

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

func TestBackPathDropsEmptyValues(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1&status=&keyword=%20%20")
	got := BackPath(c, "/admin/coupons", "project", "status", "keyword")
	if got != "/admin/coupons?project=p1" {
		t.Fatalf("期望只剩 project，实际 %q", got)
	}
}

func TestBackPathNoParams(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create")
	if got := BackPath(c, "/admin/coupons", "project"); got != "/admin/coupons" {
		t.Fatalf("期望 %q，实际 %q", "/admin/coupons", got)
	}
}

func TestBackPathEscapesValues(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?keyword="+`a%26b%3Dc`)
	got := BackPath(c, "/admin/coupons", "keyword")
	if got != "/admin/coupons?keyword=a%26b%3Dc" {
		t.Fatalf("期望转义后的值，实际 %q", got)
	}
}

func TestBackPathRejectsUnsafePath(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1")
	for _, bad := range []string{
		"//evil.example.com/x",
		"https://evil.example.com",
		"admin/coupons",
		"/admin/x\\..\\evil",
		"/admin/x\nLocation: /y",
		"/" + strings.Repeat("a", safepath.MaxReturnPathBytes+1),
	} {
		if got := BackPath(c, bad, "project"); got != adminHomePath {
			t.Fatalf("路径 %q 应当被拒绝并回 %q，实际 %q", bad, adminHomePath, got)
		}
	}
}

func TestBackPathDropsOverlongValue(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?project=p1&keyword="+strings.Repeat("k", backValueMaxBytes+1))
	got := BackPath(c, "/admin/coupons", "project", "keyword")
	if got != "/admin/coupons?project=p1" {
		t.Fatalf("超长 keyword 应当被丢弃，实际 %q", got)
	}
}

func TestBackPathDropsControlChars(t *testing.T) {
	c := backCtx(t, "/admin/coupons/create?keyword=a%0Ab&project=p1")
	got := BackPath(c, "/admin/coupons", "project", "keyword")
	if got != "/admin/coupons?project=p1" {
		t.Fatalf("含控制字符的 keyword 应当被丢弃，实际 %q", got)
	}
}

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
