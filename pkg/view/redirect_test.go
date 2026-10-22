package view

// redirect_test.go — 跳转出口的判据。
//
// 这里钉的不是「能发 3xx」，而是**状态码语义**：写后用 303、页面跳转用 302。
// 原因见 redirect.go 文件头 —— 302 在部分客户端被当成「重放原方法」，
// 「提交一次变两次」就是这么来的，而那类缺陷几乎不可能被归因到状态码。
//
// **为什么走真实 httptest.Server 而不是 gin.CreateTestContext + ResponseRecorder**：
// 实测过，后者会吃掉状态码 —— `c.Redirect(303, ...)` 走的是
// `c.Render(-1, render.Redirect{...})` → `http.Redirect(w, ...)`，而 CreateTestContext
// 返回的 ResponseWriter 在首次写头时就把状态定死成 200，记录器读到的是 200（Location
// 却是对的）。真实服务上同一个处理函数返回 303。这个差别只在测试装置里存在，
// 但它会让「状态码语义」这条判据变成一个永远绿的假测试 —— 所以这里用真实服务。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// runRedirect 起一次真实请求，返回状态码与 Location（不跟随跳转）。
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

// TestAfterWriteUses303 写后跳转必须是 303（PRG），不是 302。
//
// 302 在部分客户端会被当成「重放原方法」，于是「提交一次」变成「提交两次」——
// 表单页重复下单就是这条（block_page.go / ai_page.go 两处注释各自记过）。
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

// TestGoToUses302 页面跳转（读动作）用 302。
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

// TestRedirectIgnoresEmptyTarget 目标为空时不发跳转。
//
// 空目标写进 Location 会让浏览器跳回当前地址（看起来像「点了没反应」），
// 比不跳更难查。
func TestRedirectIgnoresEmptyTarget(t *testing.T) {
	for _, target := range []string{"", "   "} {
		code, h := runRedirect(t, http.MethodPost, "", func(c *gin.Context) {
			AfterWrite(c, target)
			GoTo(c, target)
			c.Status(http.StatusOK) // 模拟调用点继续走正常出口
		})
		if code == http.StatusSeeOther || code == http.StatusFound {
			t.Fatalf("空目标 %q 不该发跳转，实际 %d", target, code)
		}
		if loc := h.Get("Location"); loc != "" {
			t.Fatalf("空目标 %q 不该写 Location，实际 %q", target, loc)
		}
	}
}

// TestBackPassesWhitelistedKeysOnly 只透传调用点显式列出的键，且用 303。
//
// 「原样回带全部参数」会把 page=999、junk=1 之类一起带回去，
// 而「哪些参数值得保留」本来就是调用点的知识。
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

// TestBackRejectsNonLocalPath 非站内路径不跳转，由调用点决定兜底。
//
// 开放重定向在后台同样危险：`//evil.example.com` 是协议相对 URL，浏览器当外站处理。
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

// TestHXRedirectUsesHeader htmx 走 HX-Redirect + 200（它不吃 3xx 的整页跳转）。
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

// TestNilContextIsSafe 传 nil 上下文不 panic（助手在错误分支里可能被调到）。
func TestNilContextIsSafe(t *testing.T) {
	AfterWrite(nil, "/x")
	GoTo(nil, "/x")
	HXRedirect(nil, "/x")
	if Back(nil, "/x") {
		t.Fatalf("nil 上下文不该成功")
	}
}
