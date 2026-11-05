package pluginfeature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
	"regexp"

	"github.com/gin-gonic/gin"
)

type writeService struct {
	plugincontract.PluginService
	toggle    *plugindto.ToggleReq
	uninstall *plugindto.UninstallReq
	err       error
}

func (s *writeService) Toggle(_ context.Context, req *plugindto.ToggleReq) error {
	s.toggle = req
	return s.err
}
func (s *writeService) Uninstall(_ context.Context, req *plugindto.UninstallReq) error {
	s.uninstall = req
	return s.err
}

func pluginWriteTranslations(t *testing.T) {
	t.Helper()
	values := map[string]map[string]string{}
	rowRE := regexp.MustCompile(`(?m)^\('([^']+)', '([^']+)', '((?:[^']|'')*)',`)
	for _, seed := range migrations.AllSeeds() {
		for _, row := range rowRE.FindAllStringSubmatch(seed.SQL, -1) {
			if values[row[1]] == nil {
				values[row[1]] = map[string]string{}
			}
			values[row[1]][row[2]] = strings.ReplaceAll(row[3], "''", "'")
		}
	}
	i18n.InjectForTest(values, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}

func TestPluginWriteJSONTags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		typ        reflect.Type
		field, tag string
	}{
		{"toggle id", reflect.TypeOf(plugindto.ToggleReq{}), "ID", "id"},
		{"toggle enabled", reflect.TypeOf(plugindto.ToggleReq{}), "Enabled", "enabled"},
		{"uninstall id", reflect.TypeOf(plugindto.UninstallReq{}), "ID", "id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := tc.typ.FieldByName(tc.field)
			if f.Tag.Get("json") != tc.tag {
				t.Fatalf("json tag = %q, want %q", f.Tag.Get("json"), tc.tag)
			}
		})
	}
}

func TestPluginHTMLWriteContract(t *testing.T) {
	pluginWriteTranslations(t)
	for _, tc := range []struct {
		name, path, body string
		status           int
		err              error
	}{
		{"disable false", "toggle", `id=marketing&enabled=false`, 200, nil},
		{"enable true", "toggle", `id=marketing&enabled=true`, 200, nil},
		{"uninstall id", "uninstall", `id=marketing`, 200, nil},
		{"toggle missing id", "toggle", `enabled=false`, 400, nil},
		{"uninstall missing id", "uninstall", ``, 400, nil},
		{"string bool rejected", "toggle", `id=marketing&enabled=some`, 400, nil},
		{"toggle internal error", "toggle", `id=marketing&enabled=false`, 500, errors.New("SQLSTATE 23505 /private/plugin-secret")},
		{"uninstall internal error", "uninstall", `id=marketing`, 500, errors.New("SQLSTATE 23505 /private/plugin-secret")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &writeService{err: tc.err}
			h := pluginhttp.NewHandle(svc)
			r := gin.New()
			r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
			r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
			r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			r.POST("/api/plugin/toggle", pageview.HTMLFragment, h.Toggle)
			r.POST("/api/plugin/uninstall", pageview.HTMLFragment, h.Uninstall)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/plugin/"+tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("status=%d body=%q ct=%s", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
			}
			if tc.status == 400 {
				if svc.toggle != nil || svc.uninstall != nil {
					t.Fatal("invalid request reached service")
				}
				return
			}
			if tc.path == "toggle" {
				if svc.toggle == nil || svc.toggle.ID != "marketing" || svc.toggle.Enabled != strings.Contains(tc.body, `enabled=true`) {
					t.Fatalf("toggle=%+v", svc.toggle)
				}
			} else if svc.uninstall == nil || svc.uninstall.ID != "marketing" {
				t.Fatalf("uninstall=%+v", svc.uninstall)
			}
			if tc.err != nil && (strings.Contains(rec.Body.String(), "SQLSTATE") || strings.Contains(rec.Body.String(), "plugin-secret")) {
				t.Fatal("internal details leaked")
			}
		})
	}
}

func TestPluginHTMLWriteLocalizedReceipts(t *testing.T) {
	pluginWriteTranslations(t)
	for _, tc := range []struct {
		name, lang, path, want string
		err                    error
		status                 int
	}{
		{"zh toggle", "zh-CN", "toggle", "操作成功", nil, 200},
		{"en toggle", "en-US", "toggle", "Operation successful", nil, 200},
		{"zh uninstall", "zh-CN", "uninstall", "操作成功", nil, 200},
		{"en uninstall", "en-US", "uninstall", "Operation successful", nil, 200},
		{"zh unknown", "zh-CN", "toggle", "系统内部错误，请稍后重试", errors.New("SQLSTATE private details"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &writeService{err: tc.err}
			h := pluginhttp.NewHandle(svc)
			r := gin.New()
			r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
			r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
			r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			r.POST("/api/plugin/toggle", pageview.HTMLFragment, h.Toggle)
			r.POST("/api/plugin/uninstall", pageview.HTMLFragment, h.Uninstall)
			req := httptest.NewRequest("POST", "/api/plugin/"+tc.path, strings.NewReader("id=marketing&enabled=false"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Accept-Language", tc.lang)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("status=%d body=%q want %q", rec.Code, rec.Body.String(), tc.want)
			}
			if strings.Contains(rec.Body.String(), "private details") {
				t.Fatal("internal error leaked")
			}
		})
	}
}

func TestPluginJSONWriteServiceChain(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	t.Setenv("GO_WP_PLUGIN_ROOT", t.TempDir())
	row := &pluginmodel.Entity{PluginID: "writechain", Name: "Write chain", Version: "1.0.0", Enabled: true, Manifest: []byte(`{"id":"writechain","name":"Write chain","version":"1.0.0"}`)}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	h := pluginhttp.NewHandle(pluginservice.NewService(pluginmodel.NewModel(db)))
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "internal", "templates"), true)
	r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	r.POST("/api/plugin/toggle", h.Toggle)
	r.POST("/api/plugin/uninstall", h.Uninstall)
	for _, path := range []string{"toggle", "uninstall"} {
		req := httptest.NewRequest("POST", "/api/plugin/"+path, strings.NewReader("id=writechain&enabled=false"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		if path == "toggle" {
			var got pluginmodel.Entity
			if err := db.Where("plugin_id = ?", "writechain").First(&got).Error; err != nil || got.Enabled {
				t.Fatalf("disable write: %+v err=%v", got, err)
			}
		}
	}
	var count int64
	if err := db.Model(&pluginmodel.Entity{}).Where("plugin_id = ?", "writechain").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("uninstall count=%d err=%v", count, err)
	}
}

func TestPluginPageWritesUseExistingRegisteredAPI(t *testing.T) {
	r := gin.New()
	pluginhttp.SetupPluginPages(r.Group("/admin"), nil)
	for _, route := range r.Routes() {
		if route.Path == "/admin/plugins/toggle" || route.Path == "/admin/plugins/uninstall" {
			t.Fatalf("obsolete page POST registered: %s", route.Path)
		}
	}

	pluginhttp.SetupPluginRoutes(mw.NewRouteGroup(r.Group("/api"), mw.RouteProtected), nil, nil)
	for _, tc := range []struct {
		path string
		perm string
	}{{"/api/plugin/toggle", pluginenums.PluginToggle}, {"/api/plugin/uninstall", pluginenums.PluginUninstall}} {
		found := false
		for _, route := range mw.RoutesOf(tc.perm) {
			if route.Path == tc.path && route.Method == "POST" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing permission declaration: %s", tc.path)
		}
	}
}
