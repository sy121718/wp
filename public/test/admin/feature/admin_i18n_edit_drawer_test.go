package feature

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

	found := request(getURL)
	if found.Code != http.StatusOK || found.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("抽屉片段：%d %v %s", found.Code, found.Header(), found.Body.String())
	}
	fragment := strings.TrimSpace(found.Body.String())
	for _, want := range []string{
		"<div data-drawer-fragment>",
		`data-write-url="/api/i18n/save"`, `data-write="create"`,
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
		`data-write-url="/api/i18n/save"`, `data-write="bulk"`,

		"data-msg-none-selected", "data-msg-too-many",
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

	if !strings.Contains(page, "data-requires-js") {
		t.Fatalf("列表页的写控件应带 data-requires-js（无 JS 时隐藏）：%s", page)
	}
}
