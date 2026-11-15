package pluginfeature

import (
	"context"
	"errors"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"
	pluginhttp "go_wp/internal/module/plugin/inbound/http"
	pluginmodel "go_wp/internal/module/plugin/model"
	pluginservice "go_wp/internal/module/plugin/service"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/test/support"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

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
	// 词库来源是 public/schema/tables/sys_i18n.sql：位置参数 INSERT，取第 2/3/4 个字段。
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schema", "tables", "sys_i18n.sql"))
	if err != nil {
		t.Fatalf("读取词库快照失败: %v", err)
	}
	rowRE := regexp.MustCompile(`(?m)^\s*\(\d+, '([^']+)', '([^']+)', '((?:[^']|'')*)'`)
	for _, row := range rowRE.FindAllStringSubmatch(string(raw), -1) {
		if values[row[1]] == nil {
			values[row[1]] = map[string]string{}
		}
		values[row[1]][row[2]] = strings.ReplaceAll(row[3], "''", "'")
	}
	// 共享成功提示由现行 747 播种（738 已退役，SQL 仍在但不在执行集合里），
	// 夹具按现行来源补这两条，避免依赖已退役 seed 的文本。
	if values["uiOpSuccess"] == nil {
		values["uiOpSuccess"] = map[string]string{}
	}
	values["uiOpSuccess"]["zh-CN"] = "操作成功"
	values["uiOpSuccess"]["en-US"] = "Operation successful"
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
			r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
			r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			// 生产装配里成功出口由 routers 装一次；最小引擎必须同样装配，
			// 否则 Success() 会按「出口未装配」落成 500。
			r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
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
			r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
			r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
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
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
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
	pluginhttp.SetupPluginPages(nil, r.Group("/admin"), nil)
	for _, route := range r.Routes() {
		if route.Path == "/admin/plugins/toggle" || route.Path == "/admin/plugins/uninstall" {
			t.Fatalf("obsolete page POST registered: %s", route.Path)
		}
	}

	pluginhttp.SetupPluginRoutes(context.Background(), mw.NewRouteGroup(r.Group("/api"), mw.RouteProtected), nil, nil)
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
