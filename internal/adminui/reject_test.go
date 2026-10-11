package adminui_test

// reject_test.go — 页面鉴权拒绝出口（internal/adminui/reject.go）的判据，分三层。
//
//  1. HTTP 分档：401 非 htmx → 302 /admin/login；401 htmx → 200 + HX-Redirect；
//     403/500 非 htmx → **真实状态码** + Cache-Control: no-store + 渲染整页；htmx → 200 + HX-Redirect /admin。
//     壳键注入器只在「要渲染」那一档被调用，且恰好一次。
//  2. 真实 Jet 整页：用真实模板集渲染 admin/jump.html（extends layout → include partials/sidebar），
//     证明外壳键齐全（一级栏 rail、NavGroups 驱动的 data-group、HasSubnav 档的二级栏、
//     csrf_token / hx-headers / lang_redirect / 语言）与错误出口可见。假渲染器只能证明「注入了」，
//     证明不了这些 —— 缺键在 Jet 里表现为半截页面或 500，而 500 在 htmx 那一档是「点了没反应」。
//  3. 过渡期复制件防漂移：jumpView 的字段名（层2 用真实模板取 .Jump.*，层1 用反射钉字段集）、
//     HX 请求头判据（大小写 / 首尾空白矩阵，与 reqparams.IsHXRequest 同口径）。
//
// 本文件是**外部测试包 adminui_test**，且刻意**不 import internal/shell**：
//   · 内部测试包（package adminui）import shell 会与 shell → adminui 构成导入环，Go 直接拒绝编译；
//   · 外部测试包 import shell 虽然合法，但会让 scripts/check-shell-migration.sh 把本包记成
//     一个**未登记的 shell 使用 scope**（新增使用面必须先登记终局归属），而基线文件不在本批范围。
//   所以层2 的壳键夹具按模板契约手工给齐（与 internal/templates/i18n_jet_test.go:335 的
//   testNavGroup、internal/module/membership/inbound/http/membership_page_render_test.go:37-52
//   的 membershipRenderData 同一手法）：本层钉的是**模板的数据契约**，不是装配链路。
//   生产里注进这些键的是 adminui.Prepare（见 internal/shell/authz.go 的薄委托）。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"

	"go_wp/internal/adminui"
	"go_wp/internal/templates"
)

// captureRenderer 截住 gin 的渲染调用（模板名 + 数据），不做真实渲染。
type captureRenderer struct {
	called bool
	name   string
	data   any
}

func (r *captureRenderer) Instance(name string, data any) render.Render {
	r.called = true
	r.name = name
	r.data = data
	return discardRender{}
}

type discardRender struct{}

func (discardRender) Render(w http.ResponseWriter) error     { return nil }
func (discardRender) WriteContentType(_ http.ResponseWriter) {}

// countingInjector 记录壳键注入次数并原样返回数据（层1 只验「注入了、注一次」）。
func countingInjector() (adminui.ShellInjector, *int) {
	calls := new(int)
	return func(_ *gin.Context, data gin.H) gin.H {
		*calls++
		return data
	}, calls
}

// serveReject 在真实 gin 引擎上跑一次拒绝出口；hxHeader 非空则带上 HX-Request。
func serveReject(t *testing.T, status int, key, hxHeader string) (*httptest.ResponseRecorder, *captureRenderer, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := &captureRenderer{}
	engine := gin.New()
	engine.HTMLRender = r
	inject, calls := countingInjector()
	engine.GET("/reject", func(c *gin.Context) {
		adminui.Reject(inject)(c, status, key)
	})
	req := httptest.NewRequest(http.MethodGet, "/reject", nil)
	if hxHeader != "" {
		req.Header.Set("HX-Request", hxHeader)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec, r, calls
}

// TestRejectUnauthorizedBranches 未登录档：整页请求 302，htmx 请求 200 + HX-Redirect（都不渲染）。
func TestRejectUnauthorizedBranches(t *testing.T) {
	rec, r, calls := serveReject(t, http.StatusUnauthorized, "admin.perm.denied", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("未登录整页请求应当 302，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/admin/login" {
		t.Fatalf("未登录整页请求应当跳登录页，实际 Location=%q", got)
	}
	if r.called {
		t.Fatal("未登录档不该渲染任何页面（迁移前是纯重定向）")
	}
	if *calls != 0 {
		t.Fatalf("未登录档不该注入壳键，实际注入 %d 次", *calls)
	}

	rec, r, calls = serveReject(t, http.StatusUnauthorized, "admin.perm.denied", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("未登录 htmx 请求应当 200 + HX-Redirect，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/admin/login" {
		t.Fatalf("未登录 htmx 请求应当 HX-Redirect 到登录页，实际 %q", got)
	}
	if r.called || *calls != 0 {
		t.Fatal("未登录 htmx 档同样不该渲染页面或注入壳键")
	}
}

// TestRejectForbiddenAndErrorRenderWholePage 403/500 档：真实状态码 + no-store + 整页提示；
// htmx 档换 HX-Redirect（htmx 对 4xx 不 swap，整页 HTML 塞进片段位置是坏页面）。
func TestRejectForbiddenAndErrorRenderWholePage(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		// 归口文案：用一个必然不存在的词条 key，证明回落的是调用点原文而不是空串。
		const missingKey = "adminui.reject.test.missing"

		rec, r, calls := serveReject(t, status, missingKey, "")
		if rec.Code != status {
			t.Fatalf("状态 %d 档应当原样输出该状态码（200 会让失败看起来像成功），实际 %d", status, rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("状态 %d 档应当带 Cache-Control: no-store，实际 %q", status, got)
		}
		if !r.called || r.name != "admin/jump.html" {
			t.Fatalf("状态 %d 档应当渲染 admin/jump.html，实际 called=%v name=%q", status, r.called, r.name)
		}
		if *calls != 1 {
			t.Fatalf("状态 %d 档应当恰好注入一次壳键，实际 %d", status, *calls)
		}

		data, ok := r.data.(gin.H)
		if !ok {
			t.Fatalf("渲染数据应当是 gin.H，实际 %T", r.data)
		}
		title, _ := data["title"].(string)
		if title != "无权限访问" {
			t.Fatalf("词条缺失时标题应当回落调用点原文，实际 %q", title)
		}
		jump := data["Jump"]
		if jump == nil {
			t.Fatal("渲染数据缺 Jump（提示页按 .Jump.* 取名，缺失即整页中断）")
		}
		// 字段集就是模板契约：它与模板里 .Jump.* 的用法必须逐名对应。
		//
		// 这里**不再**比对「与 shell 那份复制件是否漂移」—— 复制件已删除，两边共用
		// adminui.JumpView（见 jump_page.go）。所以判据换成「类型就是那一个」：
		// 谁再新造一个同形 struct，这里立刻红。
		wantFields := map[string]string{
			"OK": "bool", "MsgKey": "string", "Msg": "string", "MsgArgs": "[]string",
			"Back": "string", "BackTextKey": "string", "BackText": "string",
			"BackTextShow": "bool", "Seconds": "int",
		}
		typ := reflect.TypeOf(jump)
		if typ.Name() != "JumpView" || typ.PkgPath() != "go_wp/internal/adminui" {
			t.Fatalf("提示页视图应当是 adminui.JumpView（唯一契约），实际 %s.%s", typ.PkgPath(), typ.Name())
		}
		if typ.NumField() != len(wantFields) {
			t.Fatalf("JumpView 字段数应为 %d，实际 %d（模板按名取，多一个少一个都要同步 jump.html）", len(wantFields), typ.NumField())
		}
		for name, kind := range wantFields {
			f, ok := typ.FieldByName(name)
			if !ok {
				t.Fatalf("JumpView 缺字段 %s（模板按名取，缺了正文/链接会静默变空）", name)
			}
			if got := f.Type.String(); got != kind {
				t.Fatalf("JumpView.%s 类型应为 %s，实际 %s", name, kind, got)
			}
		}
		val := reflect.ValueOf(jump)
		if val.FieldByName("OK").Bool() {
			t.Fatal("拒绝页必须是失败档（OK=false）")
		}
		if got := val.FieldByName("Msg").String(); got != title {
			t.Fatalf("Jump.Msg 应与标题同源，实际 %q vs %q", got, title)
		}
		if got := val.FieldByName("Back").String(); got != "/admin" {
			t.Fatalf("拒绝页回跳应当收敛到控制面首页，实际 %q", got)
		}
		if got := val.FieldByName("Seconds").Int(); got != 0 {
			t.Fatalf("拒绝页不该自动跳转（Seconds 必须 0），实际 %d", got)
		}
		if got := val.FieldByName("BackText").String(); strings.TrimSpace(got) == "" {
			t.Fatal("拒绝页的「返回后台」链接文字不能为空（空链接文字等于看不见的按钮）")
		}

		// htmx 档
		rec, r, calls = serveReject(t, status, missingKey, "true")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态 %d 的 htmx 请求应当 200 + HX-Redirect，实际 %d", status, rec.Code)
		}
		if got := rec.Header().Get("HX-Redirect"); got != "/admin" {
			t.Fatalf("状态 %d 的 htmx 请求应当 HX-Redirect 回后台首页，实际 %q", status, got)
		}
		if r.called || *calls != 0 {
			t.Fatalf("状态 %d 的 htmx 档不该渲染整页或注入壳键", status)
		}
	}
}

// TestRejectHXHeaderMatrix 钉住 HX 判据与 reqparams.IsHXRequest 同口径（大小写不敏感 + 去首尾空白）。
// 抄错一处会让分档静默失效：该跳转的改成渲染整页，htmx 那一档就是「点了没反应」。
func TestRejectHXHeaderMatrix(t *testing.T) {
	cases := []struct {
		header string
		wantHX bool
	}{
		{"true", true},
		{"TRUE", true},
		{" true ", true},
		{"false", false},
		{"1", false},
	}
	for _, tc := range cases {
		rec, _, _ := serveReject(t, http.StatusUnauthorized, "admin.perm.denied", tc.header)
		gotHX := rec.Code == http.StatusOK && rec.Header().Get("HX-Redirect") == "/admin/login"
		if gotHX != tc.wantHX {
			t.Fatalf("HX-Request=%q：期望 htmx 档 %v，实际 状态码 %d / HX-Redirect %q",
				tc.header, tc.wantHX, rec.Code, rec.Header().Get("HX-Redirect"))
		}
	}
}

// ---- 层2 的夹具：按模板契约给齐外壳键（字段名与 adminui.NavGroup / adminui.NavNode 一致；
// task-72 起侧栏视图类型归本包，此前记作 shell.NavGroup / shell.NavNode）----

// testNavNode / testNavGroup 复刻侧栏模板消费的字段（模板按名取，struct 与 map 等价）。
type testNavNode struct {
	Title    string
	Path     string
	Children []testNavNode
	Active   bool
	Open     bool
}

type testNavGroup struct {
	ID       uint64
	Title    string
	Icon     string
	IconSVG  string
	Path     string
	Nodes    []testNavNode
	Active   bool
	FirstURL string
}

// shellKeysInjector 模拟壳键注入器（生产是 adminui.Prepare）。
//
// 它顺手断言出口把自己那份数据交到了注入器手里（title + Jump 都在）——
// 出口若把数据吞掉，渲染侧就只剩壳键，页面上不会有提示内容。
func shellKeysInjector(t *testing.T) adminui.ShellInjector {
	t.Helper()
	keys := gin.H{
		"lang":          "zh-CN",
		"langs":         []any{},
		"lang_redirect": "/admin/products",
		"csrf_token":    "test-token",
		"PermSet":       map[string]bool{"product.list": true},
		"Buttons":       map[string]bool{"product.create": true},
		"NavGroups": []testNavGroup{{
			ID: 1, Title: "商品", Icon: "package", Path: "/admin/products",
			Active: true, FirstURL: "/admin/products",
			Nodes: []testNavNode{{Title: "商品列表", Path: "/admin/products", Active: true, Open: true}},
		}, {
			ID: 3, Title: "仪表盘", Icon: "gauge", Path: "/admin",
		}},
		"SidebarOpen":   true,
		"SidebarPinned": false,
		"HasSubnav":     true,
		"t":             func(_ string, fallback string) string { return fallback },
		// tf 也是壳键的一部分（Prepare 注入，见 internal/adminui/prepare.go）：
		// 提示页正文是「key + 兜底 + %s 参数」，缺了它模板整页中断。
		"tf": func(_ string, fallback string, args []string) string {
			out := fallback
			for _, a := range args {
				out = strings.Replace(out, "%s", a, 1)
			}
			return out
		},
	}
	return func(_ *gin.Context, data gin.H) gin.H {
		if title, _ := data["title"].(string); strings.TrimSpace(title) == "" {
			t.Errorf("出口没把自己的标题交给壳键注入器（渲染侧会缺 title）")
		}
		if data["Jump"] == nil {
			t.Errorf("出口没把自己的 Jump 交给壳键注入器（提示页会整页中断）")
		}
		for k, v := range keys {
			data[k] = v
		}
		return data
	}
}

// TestRejectRendersRealAdminShell 层2：真实 Jet 渲染整页（layout → sidebar → jump）。
//
// 证明的是：拒绝页拿到的外壳键齐了（侧栏 rail / NavGroups 驱动的 data-group / HasSubnav 档的
// 二级栏 / 语言与 CSRF / lang_redirect）+ 出口自己的提示内容真渲染出来了。
// 缺任何一项都会在这里红——这正是层1 用假渲染器证明不了的。
func TestRejectRendersRealAdminShell(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根相对包目录：internal/adminui → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "templates"), true)
	engine.GET("/admin/products", func(c *gin.Context) {
		adminui.Reject(shellKeysInjector(t))(c, http.StatusForbidden, "adminui.reject.test.missing")
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("整页渲染档必须是真实 403，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("整页渲染档必须 no-store，实际 %q", got)
	}

	body := rec.Body.String()
	// 半截页面的判据：Jet 在中间某行中断时不报错，只是输出停在那里。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("拒绝页不是完整页面（缺 </html>），长度 %d", len(body))
	}
	if strings.Contains(body, "{{") {
		t.Fatal("拒绝页残留模板语法（Jet 在中间某行中断）")
	}

	// 外壳键齐全 + 错误出口可见
	for _, want := range []string{
		`class="rail" id="adminRail"`,                    // 侧栏一级图标栏
		`data-group="1"`,                                 // NavGroups 注入到 DOM（侧栏数据驱动）
		`<nav class="subnav" id="subnav">`,               // HasSubnav=true 档的二级栏
		`<a class="rail-btn" href="/admin" title="仪表盘">`, // 直接链接形态的一级项（无 Nodes 时不带 data-group）
		`<html lang="zh-CN"`,                             // 语言壳键
		`<meta name="csrf-token" content="test-token">`,  // csrf_token（layout head）
		`{"X-CSRF-Token":"test-token"}`,                  // hx-headers
		`name="redirect" value="/admin/products"`,        // lang_redirect 隐藏域
		`data-jump-state="err"`,                          // 错误出口（不是 ok 档）
		`class="jump-msg">无权限访问</p>`,                     // 归口文案真的渲染到页面上
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("拒绝页缺少 %q —— 壳键或错误出口没渲染出来，body 长度 %d", want, len(body))
		}
	}
}
