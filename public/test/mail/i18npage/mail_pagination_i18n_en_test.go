package i18npage

import (
	pageview "go_wp/pkg/view"
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
	engine.GET("/admin/mail/templates", pageview.Slot(mailhttp.TemplatesPageTemplate, pageview.RenderPage), h.MailTemplatesPage)
	return engine, svc, db
}

func getPage(t *testing.T, engine *gin.Engine, path, lang string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"&lang="+lang, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s（lang=%s）应回 200，实际 %d", path, lang, rec.Code)
	}
	return rec.Body.String()
}

func TestMailTemplatesPageRendersPaginationFromRealI18n(t *testing.T) {
	engine, svc, db := newMailI18nEnv(t)

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
		`aria-label="Pagination"`,
		`>Previous<`,
		`>Next<`,
		"41 items, showing 21-40",
	} {
		if !strings.Contains(en, want) {
			t.Errorf("en-US 分页条缺少真实英文词条 %q（说明取到的是模板兜底文案或裸 key）", want)
		}
	}
	if strings.Contains(en, "shell.pagination.") {
		t.Errorf("不得渲染裸 key：%s", headOf(en))
	}

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

func pgDSN(ep support.PGEndpoint, dbname string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		ep.Host, ep.Port, ep.User, ep.Password, dbname)
}

func headOf(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
