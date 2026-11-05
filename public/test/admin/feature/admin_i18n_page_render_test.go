package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/gorm"

	"go_wp/pkg/casbin"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/public/test/support"
)

const i18nPageRenderUser = int64(99124)

const i18nPageRenderSeedKey = "page.render.sentinel"

var i18nPageRenderForbiddenRaw = []string{"sys_i18n", "42P01", "pq:", "SQLSTATE", "词条存储不可用"}

func i18nPageRenderEnv(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			// 页面标题在模板里本地取词，取词函数由请求级作用域注入 —— 与生产
			// assembly 的接线一致；缺这一步模板执行期拿不到 t，页面空 body。
			e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			pages := e.Group("/admin")
			pages.Use(func(c *gin.Context) {
				c.Set("user_id", i18nPageRenderUser)

				c.Set("perm_set", map[string]bool{"i18n:view": true, "i18n:manage": true})
				c.Set("button_set", map[string]bool{"i18n.manage": true})
				c.Next()
			})
			adminhttp.SetupAdminPages(pages, nil, nil, nil, nil, nil, nil, nil, nil)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := cleanup(); cerr != nil {
			t.Error(cerr)
		}
	})

	logDir := filepath.Join(t.TempDir(), "logs")
	base := viper.New()
	base.Set("log.base_dir", logDir)
	if err := logger.Init(base); err != nil {
		t.Fatalf("初始化日志失败：%v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("取数据库句柄失败：%v", err)
	}
	enforcer := casbin.GetEnforcer()
	if enforcer == nil {
		t.Fatal("casbin enforcer 未装配")
	}

	if _, err := enforcer.AddPolicy(strconv.FormatInt(i18nPageRenderUser, 10), "/api/i18n/list", http.MethodGet, "i18n:view"); err != nil {
		t.Fatalf("授权失败：%v", err)
	}
	return engine, db, logDir
}

func i18nPageRenderSeed(t *testing.T, db *gorm.DB, key, lang, value string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, update_time) VALUES (?, ?, ?, ?, ?, 1, 200, NOW())",
		key, lang, value, "qa", "task-95",
	).Error; err != nil {
		t.Fatalf("种子词条失败：%v", err)
	}
}

func i18nPageRenderBreakStore(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ALTER TABLE sys_i18n RENAME TO sys_i18n_render_bak").Error; err != nil {
		t.Fatalf("故障注入失败：%v", err)
	}
	t.Cleanup(func() {
		if err := db.Exec("ALTER TABLE sys_i18n_render_bak RENAME TO sys_i18n").Error; err != nil {
			t.Errorf("恢复 sys_i18n 失败：%v", err)
		}
	})
}

func i18nPageRenderGET(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{Method: http.MethodGet, Path: path})
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	return recorder
}

func i18nPageRenderAssertContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q：%s", want, body)
		}
	}
}

func i18nPageRenderAssertAbsent(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(body, bad) {
			t.Fatalf("页面不应出现 %q：%s", bad, body)
		}
	}
}

func i18nPageRenderLogHasRawError(t *testing.T, logDir, want string) bool {
	t.Helper()
	hit := false
	_ = filepath.WalkDir(logDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".log") {
			return nil
		}

		if !strings.Contains(filepath.ToSlash(path), "/i18n/") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "{") {
				continue
			}
			record := map[string]any{}
			if json.Unmarshal([]byte(line), &record) != nil {
				continue
			}
			if got, ok := record["error"].(string); ok && got == want {
				hit = true
			}
		}
		return nil
	})
	return hit
}

func TestAdminI18nEntriesPageRendersListOnSuccess(t *testing.T) {
	engine, db, _ := i18nPageRenderEnv(t)
	i18nPageRenderSeed(t, db, i18nPageRenderSeedKey, "zh-CN", "页面渲染哨兵")

	recorder := i18nPageRenderGET(t, engine, "/admin/i18n?keyword="+i18nPageRenderSeedKey)
	if recorder.Code != http.StatusOK {
		t.Fatalf("成功档应 200：%d %s", recorder.Code, recorder.Body.String())
	}
	if cache := recorder.Header().Get("Cache-Control"); strings.Contains(cache, "no-store") {
		t.Fatalf("成功档不该禁缓存：%q", cache)
	}
	body := recorder.Body.String()
	i18nPageRenderAssertContains(t, body,
		`class="data-table"`,
		i18nPageRenderSeedKey,
		"页面渲染哨兵",
		`hx-post="/api/i18n/save"`,
	)
	i18nPageRenderAssertAbsent(t, body, `data-ui-error`, "这一页的数据没能读出来", "badge-danger")
}

func i18nPageFailureMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据失败必须500: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("页面失败必须 HTML：%q", ct)
	}
	i18nPageRenderAssertContains(t, rec.Body.String(), "<!DOCTYPE html>", "data-ui-error", `role="alert"`)
	i18nPageRenderAssertAbsent(t, rec.Body.String(), append([]string{"admin.err.internal", "uiInternalErr"}, i18nPageRenderForbiddenRaw...)...)
	return i18nHTMLMessage(t, rec.Body.String())
}

func TestAdminI18nEntriesPageMainListFailureRenders500(t *testing.T) {
	engine, db, logDir := i18nPageRenderEnv(t)
	i18nPageRenderSeed(t, db, i18nPageRenderSeedKey, "zh-CN", "页面渲染哨兵")
	i18nPageRenderBreakStore(t, db)
	_, _, rawErr := i18n.ListEntries(context.Background(), i18n.EntryFilter{Limit: 20})
	if rawErr == nil {
		t.Fatal("表已改名，ListEntries 应报错")
	}
	want := rawErr.Error()
	rec := i18nPageRenderGET(t, engine, "/admin/i18n")
	if msg := i18nPageFailureMessage(t, rec); msg != "系统内部错误，请稍后重试" {
		t.Fatalf("受控中文文案不符: %q", msg)
	}
	i18nPageRenderAssertAbsent(t, rec.Body.String(), want, "页面渲染哨兵", "data-table", "tpl-i18n-create")
	if !i18nPageRenderLogHasRawError(t, logDir, want) {
		t.Fatalf("日志须保留完整驱动原文 %q", want)
	}
}

func TestAdminI18nEntriesPageStoreUnavailableRenders500(t *testing.T) {
	engine, _, logDir := i18nPageRenderEnv(t)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDB(); err == nil {
		t.Fatal("关闭后GetDB应报未初始化")
	}
	rec := i18nPageRenderGET(t, engine, "/admin/i18n")
	if msg := i18nPageFailureMessage(t, rec); msg != "系统内部错误，请稍后重试" {
		t.Fatalf("受控文案不符: %q", msg)
	}
	if !i18nPageRenderLogHasRawError(t, logDir, "词条存储不可用") {
		t.Fatal("日志须保留完整存储错误原文")
	}
}

func TestAdminI18nEntriesPageFailureFollowsRequestLanguage(t *testing.T) {
	engine, db, _ := i18nPageRenderEnv(t)
	t.Cleanup(func() {
		if err := i18n.LoadCache(); err != nil {
			t.Errorf("恢复缓存: %v", err)
		}
	})
	i18nPageRenderBreakStore(t, db)
	if msg := i18nPageFailureMessage(t, i18nPageRenderGET(t, engine, "/admin/i18n")); msg != "系统内部错误，请稍后重试" {
		t.Fatalf("中文失败文案不符: %q", msg)
	}
	if msg := i18nPageFailureMessage(t, i18nPageRenderGET(t, engine, "/admin/i18n?lang=en-US")); msg != "Internal server error, please try again later" {
		t.Fatalf("英文失败文案不符: %q", msg)
	}
	i18n.InjectForTest(map[string]map[string]string{response.InternalKey: {"zh-CN": "注入的默认语言兜底"}}, nil)
	if msg := i18nPageFailureMessage(t, i18nPageRenderGET(t, engine, "/admin/i18n?lang=en-US")); msg != "注入的默认语言兜底" {
		t.Fatalf("默认语言回退不符: %q", msg)
	}
}
