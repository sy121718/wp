package feature

// admin_i18n_page_render_test.go — 词条列表页（GET /admin/i18n）的**整页失败出口**回归（task-95）。
//
// 背景：`internal/module/admin/inbound/http/admin_page.go` 的 I18nEntriesPage 在主数据
// i18n.ListEntries 读失败时原先回 shell.PageError（JSON）。而这一页是**导航目标** ——
// 点菜单、直输 URL、刷新都落到它，响应必须自带内容（见 docs/rules/response-shapes.md 第 1/3 格）。
// 本文件钉的就是「主数据失败 → 同一模板 + 500 + no-store + 受控文案，驱动原文只进日志」。
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
	"encoding/json"
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

// i18nPageRenderForbiddenRaw 驱动原文里绝不允许进 HTML 的片段。
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
		// scene 目录：logger 按 <base>/<scene>/app-YYYY-MM-DD.log 落盘，本页的归口日志在 admin 场景。
		if !strings.Contains(filepath.ToSlash(path), "/admin/") {
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
		`data-write-api="/api/i18n/save"`, // 写通道仍在（本卡不碰它）
	)
	i18nPageRenderAssertAbsent(t, body, `role="alert"`, "这一页的数据没能读出来", "badge-danger")
}

// TestAdminI18nEntriesPageMainListFailureRenders500 主数据失败档：整页 500 + no-store，原文只进日志。
func TestAdminI18nEntriesPageMainListFailureRenders500(t *testing.T) {
	engine, db, logDir := i18nPageRenderEnv(t)
	i18nPageRenderSeed(t, db, i18nPageRenderSeedKey, "zh-CN", "页面渲染哨兵")
	i18nPageRenderBreakStore(t, db)

	// 先自己取一次真实错误原文（页面上绝不允许出现它，但日志里必须完整留下）。
	_, _, rawErr := i18n.ListEntries(context.Background(), i18n.EntryFilter{Limit: 20})
	if rawErr == nil {
		t.Fatal("表已改名，ListEntries 应报错")
	}
	want := rawErr.Error()

	recorder := i18nPageRenderGET(t, engine, "/admin/i18n")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("主数据失败应 500：%d %s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("失败档必须是整页 HTML，Content-Type=%q", ct)
	}
	if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("失败档必须禁缓存：%q", cache)
	}
	body := recorder.Body.String()
	i18nPageRenderAssertContains(t, body,
		"<!DOCTYPE", "admin-layout", `id="adminRail"`, "</html>", // 完整页壳
		`role="alert"`, // 失败槽
		"这一页的数据没能读出来",  // 失败空态标题（先于「没有匹配的词条」）
		"操作失败，请稍后重试",   // 归口文案（zh-CN 种子）
	)
	// 失败时不渲染筛选 / 表格 / 空态 / 分页 / 写表单。
	i18nPageRenderAssertAbsent(t, body,
		"没有匹配的词条",                              // 空态（另一档）
		`class="data-table"`, `name="keyword"`, // 表格与筛选
		`data-write-api="/api/i18n/save"`, `tpl-i18n-create`, // 写表单与新建抽屉
		"页面渲染哨兵", // 列表内容
	)
	// 驱动原文与裸 key 一律不许进 HTML。
	i18nPageRenderAssertAbsent(t, body, append([]string{want, "admin.err.internal", ">ErrInternal<"}, i18nPageRenderForbiddenRaw...)...)
	if !i18nPageRenderLogHasRawError(t, logDir, want) {
		t.Fatalf("日志里应留下完整原文 %q（含表名与 SQLSTATE）", want)
	}
}

// TestAdminI18nEntriesPageStoreUnavailableRenders500 词条存储不可用档：整页 500，且原文（中文哨兵）不出网。
func TestAdminI18nEntriesPageStoreUnavailableRenders500(t *testing.T) {
	engine, _, logDir := i18nPageRenderEnv(t)

	// 显式关闭句柄，并先确认全局库确实处于「未初始化」——否则这一档断言的是别的东西。
	if err := database.Close(); err != nil {
		t.Fatalf("关闭数据库失败：%v", err)
	}
	if _, err := database.GetDB(); err == nil {
		t.Fatal("关闭后 GetDB 应报未初始化")
	}

	recorder := i18nPageRenderGET(t, engine, "/admin/i18n")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("存储不可用应 500：%d %s", recorder.Code, recorder.Body.String())
	}
	if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("失败档必须禁缓存：%q", cache)
	}
	body := recorder.Body.String()
	i18nPageRenderAssertContains(t, body, "<!DOCTYPE", "admin-layout", `role="alert"`, "这一页的数据没能读出来", "操作失败，请稍后重试")
	i18nPageRenderAssertAbsent(t, body, "词条存储不可用", "没有匹配的词条", `class="data-table"`)

	want := "词条存储不可用" // pkg/i18n 的 ErrI18nUnavailable 原文
	if !i18nPageRenderLogHasRawError(t, logDir, want) {
		t.Fatalf("日志里应留下完整原文 %q", want)
	}
}

// TestAdminI18nEntriesPageFailureFollowsRequestLanguage 失败文案随请求语言，且缺词回退默认语言。
func TestAdminI18nEntriesPageFailureFollowsRequestLanguage(t *testing.T) {
	engine, db, _ := i18nPageRenderEnv(t)
	// 恢复缓存要在**表恢复之后**执行：cleanup 逆序，所以这条先登记、故障注入后登记。
	t.Cleanup(func() {
		if err := i18n.LoadCache(); err != nil {
			t.Errorf("恢复 i18n 缓存失败：%v", err)
		}
	})
	i18nPageRenderBreakStore(t, db)

	zh := i18nPageRenderGET(t, engine, "/admin/i18n")
	if zh.Code != http.StatusInternalServerError {
		t.Fatalf("zh-CN 档应 500：%d", zh.Code)
	}
	i18nPageRenderAssertContains(t, zh.Body.String(), "操作失败，请稍后重试", "这一页的数据没能读出来")
	i18nPageRenderAssertAbsent(t, zh.Body.String(), "Operation failed, please try again later")

	en := i18nPageRenderGET(t, engine, "/admin/i18n?lang=en-US")
	if en.Code != http.StatusInternalServerError {
		t.Fatalf("en-US 档应 500：%d", en.Code)
	}
	i18nPageRenderAssertContains(t, en.Body.String(), "Operation failed, please try again later", "This list could not be loaded")
	i18nPageRenderAssertAbsent(t, en.Body.String(), "操作失败，请稍后重试", ">ErrInternal<", "admin.err.internal")

	// 缺词回退：缓存里只有默认语言（zh-CN）那一条时，英文请求回退到默认语言，而不是裸 key。
	i18n.InjectForTest(map[string]map[string]string{
		"ErrInternal": {"zh-CN": "注入的默认语言兜底"},
	}, nil)
	fallback := i18nPageRenderGET(t, engine, "/admin/i18n?lang=en-US")
	if fallback.Code != http.StatusInternalServerError {
		t.Fatalf("缺词档应 500：%d", fallback.Code)
	}
	i18nPageRenderAssertContains(t, fallback.Body.String(), "注入的默认语言兜底")
	i18nPageRenderAssertAbsent(t, fallback.Body.String(), ">ErrInternal<", "Operation failed, please try again later")
}
