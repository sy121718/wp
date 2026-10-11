package feature

// admin_i18n_edit_drawer_test.go — 词条页的**读侧**回归：列表页 + 编辑抽屉片段。
//
// task-17 把词条页的四种写动作（新建 / 编辑 / 删除 / 批量删除）从页面 POST 收敛到真实 JSON 端点
// POST /api/i18n/save 之后，本文件旧的写侧断言（表单 action 回投、hx-post、csrf 隐藏域、
// 跳转提示页）整体退役 —— 它们断言的是已经不存在的通道。留下来的是读侧三件事：
//
//	① 两个读入口各自的鉴权：列表页走 adminui.PageAuthz("/api/i18n/list")，抽屉片段走
//	   CasbinMiddlewareForPathAs("/api/i18n/save","POST")（片段是写表单的宿主，所以按写权限点守门）。
//	② 抽屉片段按 (key, lang) **精确**取一条：同一 key 的另一种语言不能被串进来，且只回片段
//	   （不是整页）；缺参数 400、查不到 404，且都带 no-store（抽屉内容随权限与数据变，不许缓存）。
//	③ 片段与列表页都是 json_write.js 的宿主：必须带 data-write-api / data-write-mode / 动作标签
//	   隐藏域 / 结果区，且**不再**有 action="/admin/i18n/…" 或 hx-post 这类回投形态。
//
// 写动作的真实链路（SessionAuth / CSRF / Casbin 三层 + 落库效果 + 中英文案）见同目录
// admin_i18n_json_write_test.go。本文件不连写路径，因此不需要 Redis/会话。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// i18nDrawerUser 读侧用例的登录用户（用户 id 直接进上下文，不走会话层 —— 读侧鉴权只看 Casbin）。
const i18nDrawerUser = int64(99123)

func TestAdminI18nEditDrawerExactPairAndFragment(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			pages := e.Group("/admin")
			pages.Use(func(c *gin.Context) {
				c.Set("user_id", i18nDrawerUser)
				c.Set("perm_set", map[string]bool{"i18n:manage": true})
				// 模板读的是按钮码（adminctx.ButtonsKey），权限码集合只留给 Go 侧判断。
				c.Set("button_set", map[string]bool{"i18n.manage": true})
				c.Next()
			})
			adminhttp.SetupAdminPages(pages, nil, nil, nil, nil, nil, nil, nil, nil)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cerr := cleanup(); cerr != nil {
			t.Error(cerr)
		}
	}()
	db, err := database.GetDB()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ lang, value string }{{"zh-CN", "旧中文"}, {"en-US", "Old English"}} {
		if err := db.Exec("INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, update_time) VALUES (?, ?, ?, ?, ?, 1, 200, NOW())", "drawer.exact.test", row.lang, row.value, "old-cat", "old-note").Error; err != nil {
			t.Fatal(err)
		}
	}
	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder, reqErr := support.SendRequest(engine, support.RequestOptions{Method: http.MethodGet, Path: path})
		if reqErr != nil {
			t.Fatalf("请求失败: %v", reqErr)
		}
		return recorder
	}

	// ① 列表页要 i18n:view；抽屉片段要 i18n:manage（act=POST，见页面注册处的 ForPathAs）。
	if denied := request("/admin/i18n"); denied.Code != http.StatusForbidden {
		t.Fatalf("列表页缺 i18n:view 应 403：%d %s", denied.Code, denied.Body.String())
	}
	getURL := "/admin/i18n/edit?key=drawer.exact.test&lang=en-US"
	if denied := request(getURL); denied.Code != http.StatusForbidden {
		t.Fatalf("抽屉缺写权限点应 403：%d %s", denied.Code, denied.Body.String())
	}
	if _, err := pkgcasbin.GetEnforcer().AddPolicy("99123", "/api/i18n/save", http.MethodPost, "i18n:manage"); err != nil {
		t.Fatal(err)
	}

	// ② 精确取一条：en-US 的片段里只有英文那条，中文那条与整页外壳都不许出现。
	found := request(getURL)
	if found.Code != http.StatusOK || found.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("抽屉片段：%d %v %s", found.Code, found.Header(), found.Body.String())
	}
	fragment := strings.TrimSpace(found.Body.String())
	for _, want := range []string{
		"<div data-drawer-fragment>",
		`data-write-api="/api/i18n/save"`, `data-write-mode="create"`,
		`name="action" value="update"`,
		`name="key" value="drawer.exact.test"`, `name="lang" value="en-US"`,
		"data-write-status", "data-requires-js", "noscript",
		"Old English",
	} {
		if !strings.Contains(fragment, want) {
			t.Fatalf("抽屉片段缺少 %q：%s", want, fragment)
		}
	}
	for _, unwanted := range []string{
		`action="/admin/i18n/`, `hx-post="/admin/i18n/`, `name="csrf_token"`,
		"旧中文", "<html",
	} {
		if strings.Contains(fragment, unwanted) {
			t.Fatalf("抽屉片段不应出现 %q（写动作已收敛到 JSON 端点）：%s", unwanted, fragment)
		}
	}
	if !strings.HasSuffix(fragment, "</div>") {
		t.Fatalf("抽屉片段不是单个片段：%s", fragment)
	}

	// ③ 缺参数 400 / 查不到 404，且都 no-store。
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"缺 lang", "/admin/i18n/edit?key=drawer.exact.test", http.StatusBadRequest},
		{"缺 key", "/admin/i18n/edit?lang=en-US", http.StatusBadRequest},
		{"语言没有词条", "/admin/i18n/edit?key=drawer.exact.test&lang=fr-FR", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request(tc.path)
			if got.Code != tc.status || got.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %v %s", got.Code, got.Header(), got.Body.String())
			}
		})
	}

	// ④ 列表页：抽屉入口只带 (key, lang) —— 写动作改由浏览器直调 JSON 端点后，表单 action
	// 不再需要回带筛选与页码（保存成功整页刷新，刷新自带当前 URL）。
	if _, err := pkgcasbin.GetEnforcer().AddPolicy("99123", "/api/i18n/list", http.MethodGet, "i18n:view"); err != nil {
		t.Fatal(err)
	}
	list := request("/admin/i18n?keyword=drawer.exact.test")
	page := list.Body.String()
	if list.Code != http.StatusOK {
		t.Fatalf("列表页：%d %s", list.Code, page)
	}
	for _, want := range []string{
		`data-drawer-url="/admin/i18n/edit?key=drawer.exact.test&amp;lang=en-US"`,
		`data-write-api="/api/i18n/save"`, `data-write-mode="bulk"`,
		"data-msg-none-selected", "data-msg-partial", "data-msg-too-many",
		`name="action" value="delete"`, `name="action" value="create"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("列表页缺少 %q：%s", want, page)
		}
	}
	for _, unwanted := range []string{`action="/admin/i18n/`, `hx-post="/admin/i18n/`, "tpl-i18n-edit-"} {
		if strings.Contains(page, unwanted) {
			t.Fatalf("列表页不应出现 %q：%s", unwanted, page)
		}
	}
	// 行内删除按钮在禁用 JS 时是 hidden（有 JS 时由 json_write.js 放出来）：没有端到端兜底表单，
	// 留一个「点了只是重新加载本页」的按钮不如藏起来。
	if !strings.Contains(page, "data-requires-js") {
		t.Fatalf("列表页的写控件应带 data-requires-js（无 JS 时隐藏）：%s", page)
	}
}
