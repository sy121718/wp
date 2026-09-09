package unit

// i18n_component_test.go — 构建期组件文案的语言切换端到端验证（多语言 P4）。
//
// 与 internal/builder 的纯单测互补：这里在真实 PostgreSQL 上跑全量迁移 + 词条 seed
// （含 060 site.component.*），再经 pkg/i18n 缓存编译同一页面文档两次：
//   - zh-CN 产物出现中文文案；
//   - en-US 产物出现英文文案，且两者字节不同（同一文档、不同语言的产物可区分）。
//
// 另验证兜底链的最后一环：词条被删除（模拟缺翻译）后重新加载缓存，产物回退中文原文，
// 且绝不出现空属性或裸 key。
//
// PG 不可用时 t.Skip（与项目其他功能测试一致）。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/spf13/viper"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// i18nComponentDoc 覆盖 7 处访客面固定文案（与 internal/builder 单测同一份文档）。
const i18nComponentDoc = `{
  "settings": {"layout": {"mode": "full"}, "seo": {"title": "i18n", "description": "i18n"}},
  "root": [
    {"id": "nav1", "type": "core.nav", "props": {"items": [{"label": "Home", "url": "/"}], "mobileCollapse": true}},
    {"id": "gal1", "type": "core.gallery", "props": {"items": [{"url": "/storage/image/a.jpg", "alt": "pic"}], "mode": "carousel", "carousel": {"arrows": true, "dots": true}}},
    {"id": "sld1", "type": "core.slider", "props": {"showArrows": true, "showDots": true}, "children": [{"id": "sld1a", "type": "core.text", "props": {"text": "slide"}}]},
    {"id": "vid1", "type": "core.video", "props": {"url": "https://www.youtube.com/watch?v=PLACEHOLDER_ID"}},
    {"id": "cd1", "type": "core.countdown", "props": {"targetDate": "2026-01-01", "showDays": true}},
    {"id": "frm1", "type": "core.form", "props": {"fields": [{"type": "email", "name": "email", "label": "Email"}]}},
    {"id": "rat1", "type": "core.rating", "props": {"value": 4.5, "max": 5}}
  ]
}`

// i18nUnitTestDSN 拼装 libpq DSN（与 support.pgDSN 同构）。
func i18nUnitTestDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		support.DefaultPGHost, support.DefaultPGUser, support.DefaultPGPassword, dbname, support.DefaultPGPort)
}

// setupI18nUnitEnv 建临时库 + 全量迁移 + 词条 seed + 加载 i18n 缓存，返回库句柄。
func setupI18nUnitEnv(t *testing.T) *gorm.DB {
	t.Helper()

	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	dbName := "go_test_i18n_comp_" + hex.EncodeToString(buf)

	admin, err := gorm.Open(postgres.Open(i18nUnitTestDSN("postgres")), &gorm.Config{})
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Skipf("跳过：创建临时测试库失败: %v", err)
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
	cfg.Set("database.host", support.DefaultPGHost)
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", support.DefaultPGUser)
	cfg.Set("database.password", support.DefaultPGPassword)
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 2)
	cfg.Set("i18n.default_lang", "zh-CN")
	cfg.Set("i18n.auto_refresh", false)

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	t.Cleanup(func() {
		_ = i18n.Close()
		_ = database.Close()
	})

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("结构迁移失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("词条 seed 失败: %v", err)
	}
	if err := i18n.Init(cfg); err != nil {
		t.Fatalf("初始化 i18n 缓存失败: %v", err)
	}
	return db
}

// TestCompileComponentTextZhVsEn 同一文档按 zh-CN / en-US 各编译一次：
// 文案随语言变化、产物字节不同、同语言重复编译字节相同（确定性不受影响）。
func TestCompileComponentTextZhVsEn(t *testing.T) {
	setupI18nUnitEnv(t)

	p, err := builder.ParsePage([]byte(i18nComponentDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}

	zh, err := compile(t, p, builder.WithLanguage("zh-CN"))
	if err != nil {
		t.Fatalf("zh-CN 编译失败: %v", err)
	}
	en, err := compile(t, p, builder.WithLanguage("en-US"))
	if err != nil {
		t.Fatalf("en-US 编译失败: %v", err)
	}

	zhWants := []string{`aria-label="站点导航"`, `aria-label="上一张"`, `aria-label="下一张"`, `title="视频"`,
		`>天<`, `>时<`, `>分<`, `>秒<`, `>提交<`, `aria-label="评分 4.5 / 5"`,
		`data-slide-label="第 %s 张"`}
	for _, want := range zhWants {
		if !strings.Contains(zh.HTML, want) {
			t.Fatalf("zh-CN 产物缺少 %q\nHTML=%s", want, zh.HTML)
		}
	}

	enWants := []string{`aria-label="Site navigation"`, `aria-label="Previous"`, `aria-label="Next"`,
		`title="Video"`, `>Days<`, `>Hours<`, `>Minutes<`, `>Seconds<`, `>Submit<`, `aria-label="Rated 4.5 out of 5"`,
		`data-slide-label="Slide %s"`}
	for _, want := range enWants {
		if !strings.Contains(en.HTML, want) {
			t.Fatalf("en-US 产物缺少 %q\nHTML=%s", want, en.HTML)
		}
	}

	if zh.HTML == en.HTML {
		t.Fatal("同一文档在 zh-CN / en-US 下产物应不同（文案已走 i18n）")
	}

	// 同语言重复编译必须字节相同（构建确定性不受 i18n 影响）。
	zh2, err := compile(t, p, builder.WithLanguage("zh-CN"))
	if err != nil {
		t.Fatalf("zh-CN 二次编译失败: %v", err)
	}
	if zh.HTML != zh2.HTML || zh.CSS != zh2.CSS {
		t.Fatal("同语言重复编译产物不一致（破坏确定性）")
	}

	// 不支持的语言回退默认语言（zh-CN），不得出现空属性或裸 key。
	fallback, err := compile(t, p, builder.WithLanguage("ja-JP"))
	if err != nil {
		t.Fatalf("未知语言编译失败: %v", err)
	}
	if !strings.Contains(fallback.HTML, `aria-label="站点导航"`) {
		t.Fatalf("未知语言应回退默认语言文案\nHTML=%s", fallback.HTML)
	}
	for _, bad := range []string{`aria-label=""`, `title=""`, "site.component."} {
		if strings.Contains(fallback.HTML, bad) {
			t.Fatalf("产物出现非法输出 %q", bad)
		}
	}
}

// TestCompileComponentTextMissingKeyFallback 词条缺失（模拟未翻译 / 被删除）时，
// 产物回退组件包内中文原文，绝不输出空串或裸 key。
func TestCompileComponentTextMissingKeyFallback(t *testing.T) {
	db := setupI18nUnitEnv(t)

	if err := db.Exec("DELETE FROM sys_i18n WHERE item_key = ?", "site.component.video.title").Error; err != nil {
		t.Fatalf("删除词条失败: %v", err)
	}
	if err := i18n.Reload(); err != nil {
		t.Fatalf("重载 i18n 缓存失败: %v", err)
	}

	p, err := builder.ParsePage([]byte(i18nComponentDoc))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	res, err := compile(t, p, builder.WithLanguage("en-US"))
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}

	if !strings.Contains(res.HTML, `title="视频"`) {
		t.Fatalf("缺 en-US 词条应回退中文原文\nHTML=%s", res.HTML)
	}
	if strings.Contains(res.HTML, `title=""`) || strings.Contains(res.HTML, "site.component.video.title") {
		t.Fatalf("缺词条时产物出现空属性或裸 key\nHTML=%s", res.HTML)
	}
	// 其他词条仍有 en-US 翻译：证明是「逐条兜底」而不是整段退化。
	if !strings.Contains(res.HTML, `aria-label="Site navigation"`) {
		t.Fatalf("其余词条应仍按 en-US 输出\nHTML=%s", res.HTML)
	}
}
