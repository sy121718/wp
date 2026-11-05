package adminui

import (
	jetpkg "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	"go_wp/pkg/safepath"

	admindto "go_wp/internal/module/admin/dto"
)

func newPrepareContext(t *testing.T, target string, cookieName, cookieValue string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if cookieName != "" {
		c.Request.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	}
	return c
}

func contractNavTree() []admindto.MenuTreeNode {
	return []admindto.MenuTreeNode{
		{ID: 1, Title: "商品", Type: 1, Children: []admindto.MenuTreeNode{
			{ID: 2, Title: "商品列表", Path: "/admin/products", Type: 2},
		}},
		{ID: 3, Title: "仪表盘", Path: "/admin", Type: 2},
	}
}

func TestPrepareReadsAdminctxKeys(t *testing.T) {
	c := newPrepareContext(t, "/admin/products", "", "")
	c.Set(adminctx.PermSetKey, map[string]bool{"product.list": true})
	c.Set(adminctx.ButtonsKey, map[string]bool{"product.create": true})
	c.Set(adminctx.NavTreeKey, contractNavTree())

	data := prepare(c, gin.H{"title": "admin.products.title"})

	perms, ok := data["PermSet"].(map[string]bool)
	if !ok || !perms["product.list"] {
		t.Fatalf("Prepare 没认出 adminctx.PermSetKey（=%q）—— 键名漂移会让模板看不到权限码，data=%v",
			adminctx.PermSetKey, data["PermSet"])
	}
	buttons, ok := data["Buttons"].(map[string]bool)
	if !ok || !buttons["product.create"] {
		t.Fatalf("Prepare 没认出 adminctx.ButtonsKey（=%q）—— 按钮显隐会整块失效", adminctx.ButtonsKey)
	}
	items, ok := data["NavItems"].([]jetpkg.NavItem)
	if !ok || len(items) != 2 {
		t.Fatalf("Prepare 没认出 adminctx.NavTreeKey（=%q）—— 侧栏会空，NavItems=%v", adminctx.NavTreeKey, data["NavItems"])
	}
	if path, _ := data["NavCurrentPath"].(string); path != "/admin/products" {
		t.Fatalf("Prepare 应把当前路径作为请求事实交给模板，实际 %q", data["NavCurrentPath"])
	}
	if !jetpkg.NavBranchActive(items[0], "/admin/products") || !data["HasSubnav"].(bool) {
		t.Fatalf("当前页 /admin/products 归属「商品」分组：模板函数应判为激活且 HasSubnav 为真，实际 %+v / %v",
			items[0], data["HasSubnav"])
	}
}

type wrongPermissionMap map[string]bool

func TestPrepareFailsClosedOnInvalidContextValues(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"wrong map element", map[string]string{"product.list": "true"}},
		{"named map type", wrongPermissionMap{"product.list": true}},
		{"typed nil map", map[string]bool(nil)},
		{"wrong scalar", "product.list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPrepareContext(t, "/admin/products", "", "")
			c.Set(adminctx.PermSetKey, tc.value)
			c.Set(adminctx.ButtonsKey, tc.value)
			c.Set(adminctx.NavTreeKey, tc.value)
			data := prepare(c, nil)
			for _, key := range []string{"PermSet", "Buttons"} {
				set, ok := data[key].(map[string]bool)
				if !ok || len(set) != 0 {
					t.Fatalf("%s must fail closed: %#v", key, data[key])
				}
			}
			items, ok := data["NavItems"].([]jetpkg.NavItem)
			if !ok || len(items) != 0 || data["HasSubnav"] != false {
				t.Fatalf("navigation must fail closed: %#v", data)
			}
		})
	}
}

func TestPrepareReadsSidebarCookiesByWireName(t *testing.T) {

	pinnedCTX := newPrepareContext(t, "/admin/products", "sidebar_pinned", "1")
	if data := prepare(pinnedCTX, nil); data["SidebarPinned"] != true {
		t.Fatalf("cookie sidebar_pinned=1 应让 SidebarPinned 为真，实际 %v", data["SidebarPinned"])
	}

	closedCTX := newPrepareContext(t, "/admin/products", "sidebar_open", "0")
	if data := prepare(closedCTX, nil); data["SidebarOpen"] != false {
		t.Fatalf("cookie sidebar_open=0 应让 SidebarOpen 为假，实际 %v", data["SidebarOpen"])
	}

	defCTX := newPrepareContext(t, "/admin/products", "", "")
	defCTX.Set(adminctx.NavTreeKey, contractNavTree())
	if data := prepare(defCTX, nil); data["SidebarOpen"] != true {
		t.Fatalf("无 cookie 且当前页归属目录分组时 SidebarOpen 应为真（首次访问体验），实际 %v", data["SidebarOpen"])
	}
}

func TestPrepareInjectsI18nShellAndLangRedirect(t *testing.T) {
	c := newPrepareContext(t, "/admin/products?page=2&keyword=%E4%B9%A6", "", "")
	data := prepare(c, gin.H{"title": "admin.products.title"})

	if data["lang"] == "" || data["lang"] == nil {
		t.Fatal("lang 必须注入（模板 <html lang=…> 与语言选择器都读它）")
	}
	if data["langs"] == nil {
		t.Fatal("langs 必须注入（语言选择器选项）")
	}
	if _, ok := data["t"]; !ok {
		t.Fatal("t 必须注入（模板顶部 {{tr := .[\"t\"]}}）")
	}

	tr, ok := data["t"].(func(key, fallback string) string)
	if !ok {
		t.Fatalf("t 的签名必须是 func(key, fallback string) string，实际 %T", data["t"])
	}
	if got := tr("admin.absent.key", "兜底"); got != "兜底" {
		t.Fatalf("词条缺失时 t 必须回落 fallback，实际 %q", got)
	}

	if data["title"] != "admin.products.title" {
		t.Fatalf("标题在无词条时应原样保留，实际 %v", data["title"])
	}

	got, ok := data["lang_redirect"].(string)
	if !ok || got == "" {
		t.Fatalf("lang_redirect 必须注入，实际 %v", data["lang_redirect"])
	}
	if got != "/admin/products?page=2&keyword=%E4%B9%A6" {
		t.Fatalf("站内 URI 应原样进隐藏域（含 query），实际 %q", got)
	}
}

func TestPrepareLangRedirectFallsBackOnHostileURI(t *testing.T) {
	cases := []struct {
		name    string
		request string
	}{
		{"协议相对跳转形态", "/admin/products?next=//evil.example.com/x"},
		{"超长 query", "/admin/products?keyword=" + longQuery(600)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPrepareContext(t, tc.request, "", "")
			data := prepare(c, nil)
			got, _ := data["lang_redirect"].(string)
			if got != "/admin/products" {
				t.Fatalf("非法 URI 应回落到当前请求路径（丢 query），实际 %q", got)
			}
		})
	}
}

func TestPrepareLangRedirectFallbackChain(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		path string
		want string
	}{
		{"valid query survives", "/admin/products?project=abc&page=2", "", "/admin/products?project=abc&page=2"},
		{"overlong query drops only query", "/admin/products?project=" + strings.Repeat("x", safepath.MaxReturnPathBytes), "", "/admin/products"},
		{"overlong path falls back to admin", "/admin/" + strings.Repeat("y", safepath.MaxReturnPathBytes), "", "/admin"},
		{"protocol-relative path falls back to admin", "/admin/products", "//evil.example.com/x", "/admin"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newPrepareContext(t, tc.uri, "", "")
			if tc.path != "" {
				c.Request.URL.Path = tc.path
			}
			got, ok := prepare(c, nil)["lang_redirect"].(string)
			if !ok || got != tc.want {
				t.Fatalf("lang_redirect = %q (string=%v), want %q", got, ok, tc.want)
			}
			if tc.want == "/admin" && (strings.Contains(got, "evil.example.com") || len(got) > safepath.MaxReturnPathBytes) {
				t.Fatalf("invalid redirect leaked into hidden input: %q", got)
			}
		})
	}
}

func longQuery(n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, 'a')
	}
	return string(out)
}
