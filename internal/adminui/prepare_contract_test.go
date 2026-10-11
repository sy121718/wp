package adminui_test

// prepare_contract_test.go — 页面装配读取 adminctx 写入方声明的请求上下文键。
//
// 真正的中间件写入到 Prepare 链路由 adminctx/prepare_e2e_test.go 断言；
// 此处还检验直接注入键时的页面视图与 cookie 契约。
//
// cookie 名同理，但它们的契约对象是**浏览器与 layout 的脚本**（不是 Go 常量）：
// 用字面量 cookie 名施压，改掉就等于让老用户的侧栏态静默失效。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	"go_wp/pkg/safepath"

	admindto "go_wp/internal/module/admin/dto"
)

// newPrepareContext 造一个带请求与 cookie 的 gin 上下文。
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

// contractNavTree 授权菜单树：一个目录组（当前页归属它）+ 一个直接链接项。
func contractNavTree() []admindto.MenuTreeNode {
	return []admindto.MenuTreeNode{
		{ID: 1, Title: "商品", Type: 1, Children: []admindto.MenuTreeNode{
			{ID: 2, Title: "商品列表", Path: "/admin/products", Type: 2},
		}},
		{ID: 3, Title: "仪表盘", Path: "/admin", Type: 2},
	}
}

// TestPrepareReadsAdminctxKeys 检验三键注入后页面装配的可见产物。
func TestPrepareReadsAdminctxKeys(t *testing.T) {
	c := newPrepareContext(t, "/admin/products", "", "")
	c.Set(adminctx.PermSetKey, map[string]bool{"product.list": true})
	c.Set(adminctx.ButtonsKey, map[string]bool{"product.create": true})
	c.Set(adminctx.NavTreeKey, contractNavTree())

	data := adminui.Prepare(c, gin.H{"title": "admin.products.title"})

	perms, ok := data["PermSet"].(map[string]bool)
	if !ok || !perms["product.list"] {
		t.Fatalf("Prepare 没认出 adminctx.PermSetKey（=%q）—— 键名漂移会让模板看不到权限码，data=%v",
			adminctx.PermSetKey, data["PermSet"])
	}
	buttons, ok := data["Buttons"].(map[string]bool)
	if !ok || !buttons["product.create"] {
		t.Fatalf("Prepare 没认出 adminctx.ButtonsKey（=%q）—— 按钮显隐会整块失效", adminctx.ButtonsKey)
	}
	groups, ok := data["NavGroups"].([]adminui.NavGroup)
	if !ok || len(groups) != 2 {
		t.Fatalf("Prepare 没认出 adminctx.NavTreeKey（=%q）—— 侧栏会空，NavGroups=%v", adminctx.NavTreeKey, data["NavGroups"])
	}
	if !groups[0].Active || !data["HasSubnav"].(bool) {
		t.Fatalf("当前页 /admin/products 归属「商品」分组：NavGroups[0].Active 与 HasSubnav 都应为真，实际 %+v / %v",
			groups[0], data["HasSubnav"])
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
			data := adminui.Prepare(c, nil)
			for _, key := range []string{"PermSet", "Buttons"} {
				set, ok := data[key].(map[string]bool)
				if !ok || len(set) != 0 {
					t.Fatalf("%s must fail closed: %#v", key, data[key])
				}
			}
			groups, ok := data["NavGroups"].([]adminui.NavGroup)
			if !ok || len(groups) != 0 || data["HasSubnav"] != false {
				t.Fatalf("navigation must fail closed: %#v", data)
			}
		})
	}
}

// TestPrepareReadsSidebarCookiesByWireName 侧栏态 cookie 名是浏览器侧的契约，不能改名。
func TestPrepareReadsSidebarCookiesByWireName(t *testing.T) {
	// 固定态：sidebar_pinned=1 ⇒ SidebarPinned 为真。
	pinnedCTX := newPrepareContext(t, "/admin/products", "sidebar_pinned", "1")
	if data := adminui.Prepare(pinnedCTX, nil); data["SidebarPinned"] != true {
		t.Fatalf("cookie sidebar_pinned=1 应让 SidebarPinned 为真，实际 %v", data["SidebarPinned"])
	}
	// 收起态：sidebar_open=0 ⇒ SidebarOpen 为假（cookie 缺省才是展开）。
	closedCTX := newPrepareContext(t, "/admin/products", "sidebar_open", "0")
	if data := adminui.Prepare(closedCTX, nil); data["SidebarOpen"] != false {
		t.Fatalf("cookie sidebar_open=0 应让 SidebarOpen 为假，实际 %v", data["SidebarOpen"])
	}
	// 缺省（无 cookie）⇒ 展开；当前页归属某个目录分组时展开态不被收起
	//（`open && !pinned && !hasActiveDirGroup` 那条规则要看到活跃分组）。
	defCTX := newPrepareContext(t, "/admin/products", "", "")
	defCTX.Set(adminctx.NavTreeKey, contractNavTree())
	if data := adminui.Prepare(defCTX, nil); data["SidebarOpen"] != true {
		t.Fatalf("无 cookie 且当前页归属目录分组时 SidebarOpen 应为真（首次访问体验），实际 %v", data["SidebarOpen"])
	}
}

// TestPrepareInjectsI18nShellAndLangRedirect 多语言壳键与语言回跳隐藏域的取值。
func TestPrepareInjectsI18nShellAndLangRedirect(t *testing.T) {
	c := newPrepareContext(t, "/admin/products?page=2&keyword=%E4%B9%A6", "", "")
	data := adminui.Prepare(c, gin.H{"title": "admin.products.title"})

	if data["lang"] == "" || data["lang"] == nil {
		t.Fatal("lang 必须注入（模板 <html lang=…> 与语言选择器都读它）")
	}
	if data["langs"] == nil {
		t.Fatal("langs 必须注入（语言选择器选项）")
	}
	if _, ok := data["t"]; !ok {
		t.Fatal("t 必须注入（模板顶部 {{tr := .[\"t\"]}}）")
	}
	// i18n 未初始化时 t 回落 fallback（模板内中文原文），绝不返回空串。
	tr, ok := data["t"].(func(key, fallback string) string)
	if !ok {
		t.Fatalf("t 的签名必须是 func(key, fallback string) string，实际 %T", data["t"])
	}
	if got := tr("admin.absent.key", "兜底"); got != "兜底" {
		t.Fatalf("词条缺失时 t 必须回落 fallback，实际 %q", got)
	}
	// title 是 i18n key：词条缺失时按 fallback 原样返回（不改变行为）。
	if data["title"] != "admin.products.title" {
		t.Fatalf("标题在无词条时应原样保留，实际 %v", data["title"])
	}
	// 语言回跳隐藏域：站内 URI 原样（含 query）；判据在 pkg/safepath，两侧共用。
	got, ok := data["lang_redirect"].(string)
	if !ok || got == "" {
		t.Fatalf("lang_redirect 必须注入，实际 %v", data["lang_redirect"])
	}
	if got != "/admin/products?page=2&keyword=%E4%B9%A6" {
		t.Fatalf("站内 URI 应原样进隐藏域（含 query），实际 %q", got)
	}
}

// TestPrepareLangRedirectFallsBackOnHostileURI 非站内 / 超长 URI 必须回落当前请求路径，绝不外泄原串。
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
			data := adminui.Prepare(c, nil)
			got, _ := data["lang_redirect"].(string)
			if got != "/admin/products" {
				t.Fatalf("非法 URI 应回落到当前请求路径（丢 query），实际 %q", got)
			}
		})
	}
}

// TestPrepareLangRedirectFallbackChain pins the actual hidden input fallback chain.
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
			got, ok := adminui.Prepare(c, nil)["lang_redirect"].(string)
			if !ok || got != tc.want {
				t.Fatalf("lang_redirect = %q (string=%v), want %q", got, ok, tc.want)
			}
			if tc.want == "/admin" && (strings.Contains(got, "evil.example.com") || len(got) > safepath.MaxReturnPathBytes) {
				t.Fatalf("invalid redirect leaked into hidden input: %q", got)
			}
		})
	}
}

// longQuery 造 n 个 ASCII 字符（让 URI 超过 safepath.MaxReturnPathBytes）。
func longQuery(n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, 'a')
	}
	return string(out)
}
