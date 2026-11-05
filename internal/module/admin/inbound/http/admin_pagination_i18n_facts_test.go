package adminhttp_test

import (
	"go_wp/internal/adminui"
	"go_wp/pkg/pagination"
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
	"go_wp/internal/text"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

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

func newI18nPaginationEnv(t *testing.T) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
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

func TestI18nEntriesPagePaginationFacts(t *testing.T) {
	newI18nPaginationEnv(t)

	gin.SetMode(gin.TestMode)
	rec := &i18nPaginationRecorder{}
	engine := gin.New()
	engine.HTMLRender = rec
	h := adminhttp.NewAdminI18nEntryHandle()

	engine.GET("/admin/i18n", adminui.PageSlot(adminhttp.I18nPageTemplate), h.I18nEntriesPage)

	target := "/admin/i18n?lang=zh-CN&page=2"
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	if rec.name != adminhttp.I18nPageTemplate {
		t.Fatalf("模板名应为 %s，实际 %q", adminhttp.I18nPageTemplate, rec.name)
	}

	data := adminui.ViewMap(rec.data)
	raw, ok := data["Pagination"]
	if !ok {
		t.Fatal("词条页应注入 Pagination 事实键")
	}
	pg, ok := raw.(pagination.Page)
	if !ok {
		t.Fatalf("Pagination 应是 pagination.Page（值类型），实际 %T", raw)
	}

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

	nextWant := wantBase + "&page=3&limit=50"
	prevWant := wantBase + "&page=1&limit=50"
	renderer := templates.NewJetHTMLRender("../../../../templates", true)
	pageRec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(pageRec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	text.AttachScope(c)
	inst := renderer.Instance("admin/system/i18n", data)
	inst.WriteContentType(c.Writer)
	if err := inst.Render(c.Writer); err != nil {
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
