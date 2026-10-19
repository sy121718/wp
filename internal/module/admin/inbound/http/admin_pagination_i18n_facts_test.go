package adminhttp_test

// admin_pagination_i18n_facts_test.go — task-118 的第 7 个站点：词条页（GET /admin/i18n）。
//
// 为什么单独一个文件、且必须真库：I18nEntriesPage 的分页只数是 pkg/i18n.ListEntries 真读
// sys_i18n 的结果（total 由 SQL COUNT 给出），页数由 filter.Offset / adminI18nEntryPageSize
// 反推 —— 没有库就没有这个站点。本文件把全局 database 句柄指向**本用例自己那个已迁移库**
// （包内其它用例只用各自的 *gorm.DB，不碰全局，因此不会互相抢）。
//
// 钉四件事：
//  1. 站点注入的是事实键（Page/Limit/Base/PageParam/KeepLimit），旧成品键不出现；
//  2. total 逐字等于库里 lang='zh-CN' 的行数（证明 handler 交的是真事实，不是默认值）；
//  3. Base 带筛选（lang）—— 链接字节用 url.Values 独立拼出；
//  4. 同一份 handler 数据：事实键渲染 == 旧成品键渲染（逐字节）。

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"
	"github.com/spf13/viper"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// i18nPaginationRecorder 只记录 (模板名, data)，不出 HTML。
type i18nPaginationRecorder struct {
	name string
	data any
}

func (r *i18nPaginationRecorder) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data
	return r
}

func (r *i18nPaginationRecorder) Render(http.ResponseWriter) error     { return nil }
func (r *i18nPaginationRecorder) WriteContentType(http.ResponseWriter) {}

// newI18nPaginationEnv 真库（已迁移 + 全量 seed）+ 全局 database/i18n 指向它。
func newI18nPaginationEnv(t *testing.T) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return // NewMigratedPGTestDB 已 t.Skip
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("seed 失败：%v", err)
	}
	name := db.Migrator().CurrentDatabase()
	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.host", support.DefaultPGHost)
	cfg.Set("database.port", support.DefaultPGPort)
	cfg.Set("database.user", support.DefaultPGUser)
	cfg.Set("database.password", support.DefaultPGPassword)
	cfg.Set("database.dbname", name)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", false)
	if err := database.Init(cfg); err != nil {
		t.Fatalf("全局数据库句柄初始化失败：%v", err)
	}
	t.Cleanup(func() {
		_ = i18n.Close()
		_ = database.Close()
	})
	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("i18n 缓存初始化失败：%v", err)
	}
}

// TestI18nEntriesPagePaginationFacts 词条页只交事实，且 total 来自真库。
func TestI18nEntriesPagePaginationFacts(t *testing.T) {
	newI18nPaginationEnv(t)

	gin.SetMode(gin.TestMode)
	rec := &i18nPaginationRecorder{}
	engine := gin.New()
	engine.HTMLRender = rec
	h := adminhttp.NewAdminI18nEntryHandle()
	engine.GET("/admin/i18n", h.I18nEntriesPage)

	target := "/admin/i18n?lang=zh-CN&page=2"
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	if rec.name != "admin/system/i18n" {
		t.Fatalf("模板名应为 admin/system/i18n，实际 %q", rec.name)
	}
	data, ok := rec.data.(gin.H)
	if !ok {
		t.Fatalf("data 不是 gin.H（实际 %T）", rec.data)
	}
	raw, ok := data["Pagination"]
	if !ok {
		t.Fatal("词条页应注入 Pagination 事实键")
	}
	pg, ok := raw.(pagination.Page)
	if !ok {
		t.Fatalf("Pagination 应是 pagination.Page（值类型），实际 %T", raw)
	}

	// 独立读数：库里 zh-CN 的行数就是这次分页的分母。
	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("取全局库句柄失败：%v", err)
	}
	var wantTotal int64
	if err := db.Table("sys_i18n").Where("lang = ?", "zh-CN").Count(&wantTotal).Error; err != nil {
		t.Fatalf("统计词条数失败：%v", err)
	}
	if pg.Total != wantTotal {
		t.Fatalf("事实 total 应等于库内 zh-CN 行数 %d，实际 %d", wantTotal, pg.Total)
	}
	if pg.Limit != 50 || pg.Page != 2 {
		t.Fatalf("事实应为 limit=50 page=2，实际 limit=%d page=%d", pg.Limit, pg.Page)
	}
	if pager := templates.PagerOf(pg); pager.PageParam != "page" || !pager.KeepLimit {
		t.Fatalf("默认参数应为 page 且带 limit，实际 %q/keepLimit=%v", pager.PageParam, pager.KeepLimit)
	}
	wantBase := "/admin/i18n?lang=zh-CN"
	if pg.Base != wantBase {
		t.Fatalf("Base 应为 %q，实际 %q", wantBase, pg.Base)
	}
	if !templates.PagerOf(pg).HasPagination() {
		t.Fatalf("库里 zh-CN 词条 %d 条 / 每页 50，应多于一页（否则本用例失去意义）", wantTotal)
	}
	for _, legacy := range []string{"PaginationInfo", "PaginationLinks"} {
		if _, has := data[legacy]; has {
			t.Fatalf("旧成品键 %s 不应再注入", legacy)
		}
	}

	// 链接字节：期望值独立拼出（base 已含 ?，续接用 &）。
	nextWant := wantBase + "&page=3&limit=50"
	prevWant := wantBase + "&page=1&limit=50"
	renderer := templates.NewJetHTMLRender("../../../../templates", true)
	pageRec := httptest.NewRecorder()
	inst := renderer.Instance("admin/system/i18n", data)
	inst.WriteContentType(pageRec)
	if err := inst.Render(pageRec); err != nil {
		t.Fatalf("渲染词条页失败：%v", err)
	}
	got := pageRec.Body.String()
	if !strings.Contains(got, "href=\""+html.EscapeString(nextWant)+"\"") {
		t.Fatalf("第 2 页应出现下一页链接 %s", nextWant)
	}
	if !strings.Contains(got, "href=\""+html.EscapeString(prevWant)+"\"") {
		t.Fatalf("第 2 页应出现上一页链接 %s", prevWant)
	}
	if !strings.Contains(got, "<span class=\"pagination-btn is-active\">2</span>") {
		t.Fatal("第 2 页应是 is-active 的 2")
	}

}
