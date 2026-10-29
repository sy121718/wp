package feature

// 词条列表成功渲染HTML，主数据失败按现协议只交JSON、真实500与受控文案。
// 故障原文只进对应场景日志，不能因页面GET而切回另一套HTML失败形状。
//
// 夹具走真实链路，不用 stub：
//   · support.SetupTestBootstrap + support.NewComponentTestConfig —— 每个用例一个隔离测试库
//     （跑完生产迁移 + 种子），config.InitComponents 会把全局 database 句柄接到该库；
//   · 真实路由（adminhttp.SetupAdminPages）+ 真实 Casbin 策略 + 真实 Jet 渲染；
//   · 故障注入两种：重命名 sys_i18n 表（真实 SQL 错误，带表名与 SQLSTATE）、关闭数据库句柄
//     （pkg/i18n 的 ErrI18nUnavailable）。都不是「改代码让测试变绿」。
//
// 禁止 Parallel：本文件会改全局状态（database 句柄、i18n 缓存、logger），与同包其它用例串行执行。

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

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

// i18nPageRenderUser 词条页用例的登录用户（用户 id 直接进上下文，不走会话层 —— 读侧只看 Casbin）。
const i18nPageRenderUser = int64(99124)

// i18nPageRenderSeedKey 成功档与失败档共用的种子词条 key（失败档断言它不出现在页面上）。
const i18nPageRenderSeedKey = "page.render.sentinel"

// i18nPageRenderForbiddenRaw 驱动原文里绝不允许出现在响应中的片段。
var i18nPageRenderForbiddenRaw = []string{"sys_i18n", "42P01", "pq:", "SQLSTATE", "词条存储不可用"}

// i18nPageRenderEnv 装配真实后台词条页：隔离库 + 真实路由 + 真实 Jet + 隔离日志目录。
func i18nPageRenderEnv(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath: support.NewComponentTestConfig(t), GinMode: gin.TestMode, InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			pages := e.Group("/admin")
			pages.Use(func(c *gin.Context) {
				c.Set("user_id", i18nPageRenderUser)
				// 模板读的是按钮码（adminctx.ButtonsKey），权限码集合只留给 Go 侧判断。
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

	// 日志隔离：「原文只进日志」这条断言要读自己的目录，不能混进仓库日志或别的用例。
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
	// 列表页的权限点：adminui.PageAuthz("/api/i18n/list")（页面注册处）→ GET。
	if _, err := enforcer.AddPolicy(strconv.FormatInt(i18nPageRenderUser, 10), "/api/i18n/list", http.MethodGet, "i18n:view"); err != nil {
		t.Fatalf("授权失败：%v", err)
	}
	return engine, db, logDir
}

// i18nPageRenderSeed 落一条词条（列表页成功档要看到它）。
func i18nPageRenderSeed(t *testing.T, db *gorm.DB, key, lang, value string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, update_time) VALUES (?, ?, ?, ?, ?, 1, 200, NOW())",
		key, lang, value, "qa", "task-95",
	).Error; err != nil {
		t.Fatalf("种子词条失败：%v", err)
	}
}

// i18nPageRenderBreakStore 重命名 sys_i18n 造真实 SQL 故障，并登记恢复。
//
// 用重命名而不是 DROP：恢复是一次改名，失败信息里仍然带原表名（`relation "sys_i18n" does not exist`），
// 与「表真没了」在页面上的表现完全一致。
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

// i18nPageRenderGET 发一次真实 GET。
func i18nPageRenderGET(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder, err := support.SendRequest(engine, support.RequestOptions{Method: http.MethodGet, Path: path})
	if err != nil {
		t.Fatalf("请求失败：%v", err)
	}
	return recorder
}

// i18nPageRenderAssertContains 逐条断言「必须出现」。
func i18nPageRenderAssertContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q：%s", want, body)
		}
	}
}

// i18nPageRenderAssertAbsent 逐条断言「绝不允许出现」。
func i18nPageRenderAssertAbsent(t *testing.T, body string, unwanted ...string) {
	t.Helper()
	for _, bad := range unwanted {
		if strings.Contains(body, bad) {
			t.Fatalf("页面不应出现 %q：%s", bad, body)
		}
	}
}

// i18nPageRenderLogHasRawError 在隔离日志的 admin 场景目录里，按**逐行 JSON + 完整等值**找原文。
func i18nPageRenderLogHasRawError(t *testing.T, logDir, want string) bool {
	t.Helper()
	hit := false
	_ = filepath.WalkDir(logDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".log") {
			return nil
		}
		// 场景来自真实I18nEntriesPage的PageErrorInternal(c, "i18n", err)。
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

// TestAdminI18nEntriesPageRendersListOnSuccess 成功档回归：词条列表照常渲染，且没有失败槽。
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
		`class="data-table"`,              // 列表在
		i18nPageRenderSeedKey,             // 种子 key 在
		"页面渲染哨兵",                          // 种子内容在
		`data-write-url="/api/i18n/save"`, // 写通道仍在（本卡不碰它）
	)
	i18nPageRenderAssertAbsent(t, body, `role="alert"`, "这一页的数据没能读出来", "badge-danger")
}

// i18nPageFailureMessage 同时守住状态码、JSON形状和禁止泄漏，而非只按字符串包含判断。
func i18nPageFailureMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("主数据失败必须500: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("失败必须JSON: %q", ct)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != http.StatusInternalServerError || body.Message == "" {
		t.Fatalf("错误协议不完整: %s %v", rec.Body.String(), err)
	}
	i18nPageRenderAssertAbsent(t, rec.Body.String(), append([]string{"<!DOCTYPE", "role=\"alert\"", "admin.err.internal", "MsgInternalError"}, i18nPageRenderForbiddenRaw...)...)
	return body.Message
}

// TestAdminI18nEntriesPageMainListFailureRenders500 真实SQL故障：JSON500，原文只进日志。
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

// TestAdminI18nEntriesPageStoreUnavailableRenders500 句柄关闭同样只交受控JSON。
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

// TestAdminI18nEntriesPageFailureFollowsRequestLanguage JSON失败文案随请求语言并保默认语言回退。
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
	i18n.InjectForTest(map[string]map[string]string{"MsgInternalError": {"zh-CN": "注入的默认语言兜底"}}, nil)
	if msg := i18nPageFailureMessage(t, i18nPageRenderGET(t, engine, "/admin/i18n?lang=en-US")); msg != "注入的默认语言兜底" {
		t.Fatalf("默认语言回退不符: %q", msg)
	}
}
