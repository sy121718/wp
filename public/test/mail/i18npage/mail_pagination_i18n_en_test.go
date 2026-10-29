package i18npage

// mail_pagination_i18n_en_test.go — task-105 验收项：分页四词条在**真实英文词条**下的渲染。
//
// 为什么单独一个包（= 独立测试进程）：
//   · `pkg/i18n` 的译文缓存与 `pkg/database` 的全局句柄都是**进程级单例**，装配一次后
//     同一个测试进程里的其它用例都会看到它；
//   · 本包要的是「真库 + 真 seed + 真装载」这条链（不走 `i18n.InjectForTest` 注入），
//     把它隔离出去，`public/test/mail/feature` 那些断言中文兜底的用例才不会被影响。
//
// 判据（缺一不可）：
//   1. 41 个模板 / 每页 20 → 分页条出现，且信息行是**词条库里的英文原句**
//      `41 items, showing 21-40`（不是模板兜底的中文，也不是裸 key）；
//   2. aria-label / 上一页 / 下一页 分别取到 `Pagination` / `Previous` / `Next`；
//   3. 同一次会话里 `?lang=zh-CN` 取到中文词条 `共 41 条，第 21-40 条` —— 两边都来自
//      `sys_i18n` 行，才能证明「真词条生效」而不是「碰巧命中了兜底文案」；
//   4. 迁移前那条坏链接（模板页链接不带 ?page）必须已修好：next 指向 `?page=3`。

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/templates"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newMailI18nEnv 建一个**独立临时库**，跑结构迁移 + 全量 seed，再用真装配装载 i18n 缓存。
func newMailI18nEnv(t *testing.T) (*gin.Engine, *mailservice.Service, *gorm.DB) {
	t.Helper()

	ep, _, err := support.AcquirePG(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过分页英文词条用例：%v", err)
	}
	dbName := fmt.Sprintf("go_test_maili18n_%d", rand.Int63n(1_000_000_000))

	admin, err := gorm.Open(postgres.Open(pgDSN(ep, "postgres")), &gorm.Config{})
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("连接管理连接失败，跳过：%v", err)
	}
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("创建临时测试库失败，跳过：%v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName)
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", dbName)
	cfg.Set("database.host", ep.Host)
	cfg.Set("database.port", ep.Port)
	cfg.Set("database.user", ep.User)
	cfg.Set("database.password", ep.Password)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", false)

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库组件失败：%v", err)
	}
	t.Cleanup(func() {
		_ = i18n.Close()
		_ = database.Close()
	})

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败：%v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("结构迁移失败：%v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("seed 失败：%v", err)
	}
	// 真装载：LoadCache 读 database.GetDB() 的 sys_i18n 行（pkg/i18n/loader.go）。
	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("初始化 i18n 缓存失败：%v", err)
	}

	svc := mailservice.NewService(mailmodel.NewMailModel(db))
	svc.SetCipherSecret("mail-i18n-page-test")

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, ge := range c.Errors {
			t.Logf("gin 渲染错误: %v", ge.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(svc)
	// 页面出口必须与生产路由一致（adminui.PageAuthz → adminui.PageSlot → handler）：
	// 只挂 handler 时 view.Render 拿不到出口，报「视图出口未装配」→ 500，
	// 而本用例要证的是「真词条下的分页文案」，会被这个夹具缺口盖掉。
	engine.GET("/admin/mail/templates",
		adminui.PageSlot(mailhttp.TemplatesPageTemplate), h.MailTemplatesPage)
	return engine, svc, db
}

// getPage 发一次页面请求（lang 同步进 query —— response.RequestLanguage 的取值链是
// cookie → query lang → Accept-Language，这里用 query 明确指定）。
func getPage(t *testing.T, engine *gin.Engine, path, lang string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"&lang="+lang, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s（lang=%s）应回 200，实际 %d", path, lang, rec.Code)
	}
	return rec.Body.String()
}

// TestMailTemplatesPageRendersPaginationFromRealI18n 真词条（迁移 059）在 en-US / zh-CN 下都生效。
func TestMailTemplatesPageRendersPaginationFromRealI18n(t *testing.T) {
	engine, svc, db := newMailI18nEnv(t)

	// 夹具必须**恰好 41 条**：断言里的「21-40」正是 41 / 每页 20 的第 2 页。
	// 全量 seed 会带几条内置模板，所以先数现有条数再补足 —— 不写死种子条数，
	// 种子增删不会让这条用例变成「数字对不上」的假红。
	const wantTotal = 41
	var existing int64
	if err := db.Table("mail_templates").Count(&existing).Error; err != nil {
		t.Fatalf("统计现有邮件模板失败：%v", err)
	}
	if existing > wantTotal {
		t.Fatalf("现有模板 %d 条已超过夹具上限 %d：本用例的分页断言不再成立，请调整夹具", existing, wantTotal)
	}
	for i := existing; i < wantTotal; i++ {
		if _, err := svc.UpsertTemplate(context.Background(), &maildto.SaveTemplateReq{
			TemplateKey: fmt.Sprintf("i18n_tpl_%02d", i), Name: "英文分页模板", Subject: "Subject",
			BodyHTML: "<p>x</p>",
		}); err != nil {
			t.Fatalf("造第 %d 个模板失败：%v", i, err)
		}
	}

	en := getPage(t, engine, "/admin/mail/templates?page=2", "en-US")
	for _, want := range []string{
		`aria-label="Pagination"`, // shell.pagination.label
		`>Previous<`,              // shell.pagination.prev
		`>Next<`,                  // shell.pagination.next
		"41 items, showing 21-40", // shell.pagination.info（%s items, showing %s-%s）
	} {
		if !strings.Contains(en, want) {
			t.Errorf("en-US 分页条缺少真实英文词条 %q（说明取到的是模板兜底文案或裸 key）", want)
		}
	}
	if strings.Contains(en, "shell.pagination.") {
		t.Errorf("不得渲染裸 key：%s", headOf(en))
	}
	// 追认修复：模板页翻页链接必须带 ?page=N（修复前全部退化成不带 page 的基址）。
	if !strings.Contains(en, "page=3") {
		t.Errorf("模板页「下一页」应带 ?page=3（task-105 追认修复）：%s", headOf(en))
	}

	zh := getPage(t, engine, "/admin/mail/templates?page=2", "zh-CN")
	for _, want := range []string{"共 41 条，第 21-40 条", "上一页", "下一页", `aria-label="分页"`} {
		if !strings.Contains(zh, want) {
			t.Errorf("zh-CN 分页条缺少词条 %q：%s", want, headOf(zh))
		}
	}
}

// pgDSN 拼连接串（database=空时用传入的库名）。
func pgDSN(ep support.PGEndpoint, dbname string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		ep.Host, ep.Port, ep.User, ep.Password, dbname)
}

// headOf 截断输出，失败信息里不刷整页 HTML。
func headOf(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
