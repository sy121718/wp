package view

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func runRedirect(t *testing.T, method, rawURL string, handle gin.HandlerFunc) (int, http.Header) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Any("/probe", handle)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(method, srv.URL+"/probe"+rawURL, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header
}

func TestAfterWriteUses303(t *testing.T) {
	code, h := runRedirect(t, http.MethodPost, "", func(c *gin.Context) {
		AfterWrite(c, "/admin/orders")
	})
	if code != http.StatusSeeOther {
		t.Fatalf("写后跳转应是 303，实际 %d", code)
	}
	if loc := h.Get("Location"); loc != "/admin/orders" {
		t.Fatalf("Location 不对：%q", loc)
	}
}

func TestGoToUses302(t *testing.T) {
	code, h := runRedirect(t, http.MethodGet, "", func(c *gin.Context) {
		GoTo(c, "/admin/y")
	})
	if code != http.StatusFound {
		t.Fatalf("页面跳转应是 302，实际 %d", code)
	}
	if loc := h.Get("Location"); loc != "/admin/y" {
		t.Fatalf("Location 不对：%q", loc)
	}
}

func TestRedirectIgnoresEmptyTarget(t *testing.T) {
	for _, target := range []string{"", "   "} {
		code, h := runRedirect(t, http.MethodPost, "", func(c *gin.Context) {
			AfterWrite(c, target)
			GoTo(c, target)
			c.Status(http.StatusOK)
		})
		if code == http.StatusSeeOther || code == http.StatusFound {
			t.Fatalf("空目标 %q 不该发跳转，实际 %d", target, code)
		}
		if loc := h.Get("Location"); loc != "" {
			t.Fatalf("空目标 %q 不该写 Location，实际 %q", target, loc)
		}
	}
}

func TestBackPassesWhitelistedKeysOnly(t *testing.T) {
	code, h := runRedirect(t, http.MethodPost, "?status=paid&page=3&junk=1", func(c *gin.Context) {
		if !Back(c, "/admin/orders", "status", "page") {
			c.Status(http.StatusInternalServerError)
		}
	})
	if code != http.StatusSeeOther {
		t.Fatalf("回跳是写后跳转，应 303，实际 %d", code)
	}
	loc := h.Get("Location")
	if loc != "/admin/orders?page=3&status=paid" {
		t.Fatalf("回跳地址应按白名单透传且按 Encode 排序，实际 %q", loc)
	}
}

func TestBackRejectsNonLocalPath(t *testing.T) {
	for _, path := range []string{"//evil.example.com/x", "https://evil.example.com", "javascript:alert(1)"} {
		var returned bool
		code, h := runRedirect(t, http.MethodPost, "?a=1", func(c *gin.Context) {
			returned = Back(c, path, "a")
			c.Status(http.StatusOK)
		})
		if returned {
			t.Fatalf("非站内路径 %q 不该返回成功", path)
		}
		if code == http.StatusSeeOther || code == http.StatusFound {
			t.Fatalf("非站内路径 %q 不该发跳转，实际 %d", path, code)
		}
		if loc := h.Get("Location"); loc != "" {
			t.Fatalf("非站内路径 %q 不该写 Location，实际 %q", path, loc)
		}
	}
}

func TestHXRedirectUsesHeader(t *testing.T) {
	code, h := runRedirect(t, http.MethodPost, "", func(c *gin.Context) {
		HXRedirect(c, "/admin/orders")
	})
	if code != http.StatusOK {
		t.Fatalf("HX-Redirect 应配 200，实际 %d", code)
	}
	if loc := h.Get("HX-Redirect"); loc != "/admin/orders" {
		t.Fatalf("HX-Redirect 头不对：%q", loc)
	}
	if loc := h.Get("Location"); loc != "" {
		t.Fatalf("不该同时写 Location，实际 %q", loc)
	}
}

func TestNilContextIsSafe(t *testing.T) {
	AfterWrite(nil, "/x")
	GoTo(nil, "/x")
	HXRedirect(nil, "/x")
	if Back(nil, "/x") {
		t.Fatalf("nil 上下文不该成功")
	}
}
