package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

func TestAdminI18nEditDrawerExactPairAndWrite(t *testing.T) {
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			pages := e.Group("/admin")
			pages.Use(func(c *gin.Context) {
				c.Set("user_id", int64(99123))
				c.Set("perm_set", map[string]bool{"i18n:manage": true})
				c.Next()
			})
			adminhttp.SetupAdminPages(pages, nil, nil, nil, nil, nil, nil, nil)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			t.Error(err)
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
	request := func(method, path string, form url.Values, hx bool) *httptest.ResponseRecorder {
		t.Helper()
		var body *strings.Reader
		if form == nil {
			body = strings.NewReader("")
		} else {
			body = strings.NewReader(form.Encode())
		}
		req := httptest.NewRequest(method, path, body)
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if hx {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}
	getURL := "/admin/i18n/edit?key=drawer.exact.test&lang=en-US&keyword=drawer&filter_lang=en-US&page=2"
	noView := request(http.MethodGet, "/admin/i18n", nil, false)
	if noView.Code != http.StatusForbidden {
		t.Fatalf("list without view permission: %d %s", noView.Code, noView.Body.String())
	}
	denied := request(http.MethodGet, getURL, nil, false)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("GET missing permission: %d %s", denied.Code, denied.Body.String())
	}
	if _, err := pkgcasbin.GetEnforcer().AddPolicy("99123", "/api/i18n/save", "POST", "i18n:manage"); err != nil {
		t.Fatal(err)
	}
	found := request(http.MethodGet, getURL, nil, false)
	if found.Code != 200 || found.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET: %d %v %s", found.Code, found.Header(), found.Body.String())
	}
	html := strings.TrimSpace(found.Body.String())
	for _, want := range []string{"<div data-drawer-fragment>", `action="/admin/i18n/update"`, `hx-post="/admin/i18n/update"`, `name="csrf_token"`, `name="key" value="drawer.exact.test"`, `name="lang" value="en-US"`, `name="_keyword" value="drawer"`, `name="_lang" value="en-US"`, `name="_page" value="2"`, "Old English"} {
		if !strings.Contains(html, want) {
			t.Fatalf("GET missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "旧中文") || strings.Contains(html, "<html") || !strings.HasSuffix(html, "</div>") {
		t.Fatalf("GET not exact single fragment: %s", html)
	}
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"empty key", "/admin/i18n/edit?lang=en-US", 400},
		{"empty lang", "/admin/i18n/edit?key=drawer.exact.test", 400},
		{"unknown pair", "/admin/i18n/edit?key=drawer.exact.test&lang=fr-FR", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request(http.MethodGet, tc.path, nil, false)
			if got.Code != tc.status || got.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %v %s", got.Code, got.Header(), got.Body.String())
			}
		})
	}
	if _, err := pkgcasbin.GetEnforcer().AddPolicy("99123", "/api/i18n/list", "GET", "i18n:view"); err != nil {
		t.Fatal(err)
	}
	list := request(http.MethodGet, "/admin/i18n?keyword=drawer.exact.test", nil, false)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `data-drawer-url="/admin/i18n/edit?key=drawer.exact.test&amp;keyword=drawer.exact.test&amp;lang=en-US"`) || strings.Contains(list.Body.String(), `tpl-i18n-edit-`) {
		t.Fatalf("list drawer source: %d %s", list.Code, list.Body.String())
	}
	form := url.Values{"key": {"drawer.exact.test"}, "lang": {"en-US"}, "value": {""}, "category": {"changed-cat"}, "remark": {"changed-note"}, "_keyword": {"drawer"}, "_page": {"2"}}
	failed := request(http.MethodPost, "/admin/i18n/update", form, true)
	if failed.Code != 200 || failed.Header().Get("HX-Redirect") != "" || !strings.Contains(failed.Body.String(), `role="alert"`) || !strings.Contains(failed.Body.String(), `value="changed-cat"`) || !strings.Contains(failed.Body.String(), `value="changed-note"`) || !strings.Contains(failed.Body.String(), `name="csrf_token"`) || strings.Contains(failed.Body.String(), "<html") {
		t.Fatalf("HX failure: %d %v %s", failed.Code, failed.Header(), failed.Body.String())
	}
	form.Set("value", "New <translation> & user text")
	form.Set("lang", "fr-FR")
	missing := request(http.MethodPost, "/admin/i18n/update", form, true)
	if missing.Code != 200 || missing.Header().Get("HX-Redirect") == "" {
		t.Fatalf("missing pair must not upsert: %d %v %s", missing.Code, missing.Header(), missing.Body.String())
	}
	if _, err := i18n.GetEntry(t.Context(), "drawer.exact.test", "fr-FR"); err == nil {
		t.Fatal("missing pair was created")
	}
	form.Set("lang", "en-US")
	form.Set("value", "   ")
	echo := request(http.MethodPost, "/admin/i18n/update", form, true)
	if echo.Code != 200 || !strings.Contains(echo.Body.String(), `<textarea name="value" class="form-textarea" rows="3" required>   </textarea>`) || !strings.Contains(echo.Body.String(), `role="alert"`) {
		t.Fatalf("whitespace value echo: %d %s", echo.Code, echo.Body.String())
	}
	form.Set("value", "New <translation> & user text")
	saved := request(http.MethodPost, "/admin/i18n/update", form, true)
	if saved.Code != 200 || !strings.HasPrefix(saved.Header().Get("HX-Redirect"), "/admin/i18n?") || saved.Header().Get("Location") != "" {
		t.Fatalf("HX success: %d %v %s", saved.Code, saved.Header(), saved.Body.String())
	}
	entry, err := i18n.GetEntry(t.Context(), "drawer.exact.test", "en-US")
	if err != nil || entry.Value != form.Get("value") || entry.Category != "changed-cat" || entry.Remark != "changed-note" {
		t.Fatalf("exact update: %+v %v", entry, err)
	}
	other, err := i18n.GetEntry(t.Context(), "drawer.exact.test", "zh-CN")
	if err != nil || other.Value != "旧中文" {
		t.Fatalf("other language changed: %+v %v", other, err)
	}
	form.Set("value", "")
	nativeFailed := request(http.MethodPost, "/admin/i18n/update", form, false)
	if nativeFailed.Code != http.StatusFound || !strings.Contains(nativeFailed.Header().Get("Location"), "errored=") {
		t.Fatalf("native failure: %d %v", nativeFailed.Code, nativeFailed.Header())
	}
	form.Set("value", "Native saved")
	native := request(http.MethodPost, "/admin/i18n/update", form, false)
	if native.Code != http.StatusSeeOther || native.Header().Get("HX-Redirect") != "" || !strings.HasPrefix(native.Header().Get("Location"), "/admin/i18n?") {
		t.Fatalf("native success: %d %v", native.Code, native.Header())
	}
}
