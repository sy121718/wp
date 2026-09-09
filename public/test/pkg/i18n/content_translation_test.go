package i18n_test

// content_translation_test.go — 内容翻译基础设施（多语言 P5a，docs/06-D-site-i18n.md §7）
// 在真实 PostgreSQL 上的链路验证。
//
// 覆盖（贴真实输出见交付报告）：
//  1. 迁移 066 幂等：全部迁移连跑两遍不报错，表/索引/CHECK 约束落库，约束真实生效；
//  2. 批量取数：按 (hashes[], lang) 一条 SQL 取回全部命中（用计数 logger 断言只发 1 条查询）；
//  3. 取词链路：命中用译文、未命中回退原文、同文本不同 context 分别翻译、跳过规则不进取词；
//  4. 兜底：表缺失 / 数据库未初始化时查询层返回 error、取词层静默回退原文（不报错、不 panic、不返回空串）；
//  5. 自动失效：改源文本 → hash 变 → 旧译文不命中（旧行保留可追溯）。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// countingLogger 统计命中 sys_translation 的查询次数（验证「不逐条查库」）。
type countingLogger struct {
	mu    sync.Mutex
	query int
}

func (l *countingLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }
func (l *countingLogger) Info(context.Context, string, ...interface{})     {}
func (l *countingLogger) Warn(context.Context, string, ...interface{})     {}
func (l *countingLogger) Error(context.Context, string, ...interface{})    {}

func (l *countingLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.Contains(strings.ToLower(sql), "from sys_translation") {
		l.mu.Lock()
		l.query++
		l.mu.Unlock()
	}
}

func (l *countingLogger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.query
}

// insertTranslation 写入一行译文（source_text 与 hash 必须一致，与工作台写入口径相同）。
func insertTranslation(t *testing.T, db *gorm.DB, sourceText, contextName, lang, targetText, engine string) {
	t.Helper()
	err := db.Exec("INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text, engine, updated_at) VALUES (?, ?, ?, ?, ?, ?, now())",
		i18n.ContentHash(sourceText), contextName, lang, sourceText, targetText, engine).Error
	if err != nil {
		t.Fatalf("写入译文 (%q,%q,%q) 失败: %v", sourceText, contextName, lang, err)
	}
}

// TestSysTranslationMigrationIdempotency 迁移 066 幂等 + 结构 + 约束真实生效。
func TestSysTranslationMigrationIdempotency(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}

	// 全部迁移连跑两遍：第二遍应命中幂等守卫（066 为「表存在即跳过」），不报错。
	for round := 1; round <= 2; round++ {
		if err := migrations.Run(db); err != nil {
			t.Fatalf("第 %d 轮迁移失败: %v", round, err)
		}
	}

	// 1) 列结构（§7.3）
	var cols []string
	if err := db.Raw("SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'sys_translation'").Scan(&cols).Error; err != nil {
		t.Fatalf("查询列失败: %v", err)
	}
	want := []string{"source_hash", "context", "lang", "source_text", "target_text", "engine", "updated_at"}
	for _, c := range want {
		found := false
		for _, got := range cols {
			if got == c {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("sys_translation 缺少列 %s，实际列: %v", c, cols)
		}
	}

	// 2) 索引（§7.3）
	for _, idx := range []string{"idx_sys_translation_hash_lang", "idx_sys_translation_context_lang", "idx_sys_translation_engine_lang"} {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'sys_translation' AND indexname = ?", idx).Scan(&n).Error; err != nil {
			t.Fatalf("查询索引失败: %v", err)
		}
		if n != 1 {
			t.Fatalf("缺少索引 %s", idx)
		}
	}

	// 3) CHECK 约束（§7.3）
	for _, ck := range []string{"ck_sys_translation_hash", "ck_sys_translation_engine", "ck_sys_translation_target"} {
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM pg_constraint WHERE conname = ? AND conrelid = 'sys_translation'::regclass", ck).Scan(&n).Error; err != nil {
			t.Fatalf("查询约束失败: %v", err)
		}
		if n != 1 {
			t.Fatalf("缺少约束 %s", ck)
		}
	}

	// 4) 约束真实生效：非法 hash / 非法 engine / 空译文 均被拒。
	if err := db.Exec("INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text) VALUES ('NOT-A-HASH','core.button.text','en-US','x','y')").Error; err == nil {
		t.Fatalf("非法 source_hash 应被 ck_sys_translation_hash 拒绝")
	}
	if err := db.Exec("INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text, engine) VALUES (?, 'core.button.text','en-US','x','y','machine')", i18n.ContentHash("x")).Error; err == nil {
		t.Fatalf("非法 engine 应被 ck_sys_translation_engine 拒绝")
	}
	if err := db.Exec("INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text) VALUES (?, 'core.button.text','en-US','x','')", i18n.ContentHash("x")).Error; err == nil {
		t.Fatalf("空 target_text 应被 ck_sys_translation_target 拒绝")
	}

	// 5) engine 默认 manual；主键三元组唯一（同 hash 同 context 同语言只能一行）。
	insertTranslation(t, db, "了解更多", "core.button.text", "en-US", "Learn more", "manual")
	var engine string
	if err := db.Raw("SELECT engine FROM sys_translation WHERE lang = 'en-US'").Scan(&engine).Error; err != nil {
		t.Fatalf("读取 engine 失败: %v", err)
	}
	if engine != "manual" {
		t.Fatalf("engine 默认值应为 manual，实际 %q", engine)
	}
	if err := db.Exec("INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text) VALUES (?, 'core.button.text','en-US','了解更多','Dup')", i18n.ContentHash("了解更多")).Error; err == nil {
		t.Fatalf("同 (source_hash, context, lang) 重复写入应被主键拒绝")
	}
}

// TestContentBatchQuerySingleSQL 批量取数：一次 SQL 取回全部命中，不逐条查库。
func TestContentBatchQuerySingleSQL(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	insertTranslation(t, db, "了解更多", "core.button.text", "en-US", "Learn more", "manual")
	insertTranslation(t, db, "了解更多", "product.cta", "en-US", "Shop now", "ai")
	insertTranslation(t, db, "联系我们", "core.button.text", "en-US", "Contact us", "manual")
	insertTranslation(t, db, "了解更多", "core.button.text", "zh-CN", "了解更多-zh", "manual")

	hashes := []string{
		i18n.ContentHash("了解更多"),
		i18n.ContentHash("联系我们"),
		i18n.ContentHash("从未翻译过的文本"),
	}

	// 查询层：一次调用返回 hash+context → target 的映射（zh-CN 行被 lang 过滤）。
	targets, err := i18n.NewDBContentStore(db).LoadTargets(context.Background(), "en-US", hashes)
	if err != nil {
		t.Fatalf("批量查询失败: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("应命中 3 条，实际 %d: %v", len(targets), targets)
	}
	if got := targets[i18n.ContentIndexKey(i18n.ContentHash("了解更多"), "core.button.text")]; got != "Learn more" {
		t.Fatalf("core.button.text 命中错误: %q", got)
	}
	if got := targets[i18n.ContentIndexKey(i18n.ContentHash("了解更多"), "product.cta")]; got != "Shop now" {
		t.Fatalf("product.cta 命中错误: %q", got)
	}
	if _, ok := targets[i18n.ContentIndexKey(i18n.ContentHash("从未翻译过的文本"), "core.button.text")]; ok {
		t.Fatalf("未翻译文本不应命中")
	}

	// 取词层：构造一次只发一条 SQL（计数 logger 断言），取词期不再查库。
	cl := &countingLogger{}
	counted := db.Session(&gorm.Session{Logger: cl})
	tr := i18n.NewContentTranslatorWith(context.Background(), i18n.NewDBContentStore(counted), "en-US", hashes)

	if got := cl.count(); got != 1 {
		t.Fatalf("取词器构造应只发 1 条 sys_translation 查询，实际 %d 条", got)
	}
	if tr.Size() != 3 {
		t.Fatalf("索引应命中 3 条，实际 %d", tr.Size())
	}
	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "Learn more" {
		t.Fatalf("命中取词错误: %q", got)
	}
	if got := cl.count(); got != 1 {
		t.Fatalf("取词期不应再发查询，实际累计 %d 条", got)
	}
}

// TestContentTranslateHitFallbackAndSkipRule 真实表上的取词链路：命中 / 回退 / 跳过 / 不跨语境。
func TestContentTranslateHitFallbackAndSkipRule(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	insertTranslation(t, db, "了解更多", "core.button.text", "en-US", "Learn more", "manual")
	insertTranslation(t, db, "共 42 件", "product.stock", "en-US", "42 items", "ai")

	ctx := context.Background()
	hashes := []string{
		i18n.ContentHash("了解更多"),
		i18n.ContentHash("共 42 件"),
		i18n.ContentHash("2024"),
		i18n.ContentHash("→"),
	}
	tr := i18n.NewContentTranslatorWith(ctx, i18n.NewDBContentStore(db), "en-US", hashes)

	// 命中。
	if got := tr.TranslateContent("了解更多", i18n.ContentContext("core.button", "text")); got != "Learn more" {
		t.Fatalf("命中应返回译文，实际 %q", got)
	}
	// 含数字的句子照常翻译。
	if got := tr.TranslateContent("共 42 件", "product.stock"); got != "42 items" {
		t.Fatalf("含数字的句子应命中译文，实际 %q", got)
	}
	// 同文本不同 context：不跨语境回退。
	if got := tr.TranslateContent("了解更多", "product.cta"); got != "了解更多" {
		t.Fatalf("跨语境应回退原文，实际 %q", got)
	}
	// 跳过规则：纯数字 / 纯符号原样返回，且不计入缺失。
	before := tr.Misses()
	if got := tr.TranslateContent("2024", "core.heading.text"); got != "2024" {
		t.Fatalf("纯数字应原样返回，实际 %q", got)
	}
	if got := tr.TranslateContent("→", "core.button.text"); got != "→" {
		t.Fatalf("纯符号应原样返回，实际 %q", got)
	}
	if tr.Misses() != before {
		t.Fatalf("跳过取值不应计入缺失，实际 %d → %d", before, tr.Misses())
	}
	// 未命中回退原文，绝不空串。
	if got := tr.TranslateContent("从未翻译过的文本", "core.heading.text"); got != "从未翻译过的文本" {
		t.Fatalf("未命中应回退原文，实际 %q", got)
	}
	if tr.Misses() == before {
		t.Fatalf("未命中应计入缺失计数")
	}
}

// TestContentTranslationTableMissingFallsBack 表缺失兜底：查询层返回 error、取词层回退原文。
func TestContentTranslationTableMissingFallsBack(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	// 刻意不跑迁移：该隔离 schema 里没有 sys_translation。

	ctx := context.Background()
	hashes := []string{i18n.ContentHash("了解更多")}

	if _, err := i18n.NewDBContentStore(db).LoadTargets(ctx, "en-US", hashes); err == nil {
		t.Fatalf("表缺失时查询层应返回 error")
	}

	tr := i18n.NewContentTranslatorWith(ctx, i18n.NewDBContentStore(db), "en-US", hashes)
	if tr.Size() != 0 {
		t.Fatalf("表缺失时应得空索引，实际 %d", tr.Size())
	}
	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "了解更多" {
		t.Fatalf("表缺失应回退原文，实际 %q", got)
	}
	if got := tr.TranslateContent("2024", "core.button.text"); got != "2024" {
		t.Fatalf("表缺失时纯数字仍原样返回，实际 %q", got)
	}
}

// TestContentHashChangeInvalidatesOldTranslation 改源文本 → hash 变 → 旧译文不命中（旧行保留）。
func TestContentHashChangeInvalidatesOldTranslation(t *testing.T) {
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("跳过：本地 PostgreSQL 不可用: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	insertTranslation(t, db, "了解更多", "core.button.text", "en-US", "Learn more", "manual")

	ctx := context.Background()
	oldText := "了解更多"
	newText := "了解更多详情"

	// 原文未变：命中。
	oldTranslator := i18n.NewContentTranslatorWith(ctx, i18n.NewDBContentStore(db), "en-US", []string{i18n.ContentHash(oldText)})
	if got := oldTranslator.TranslateContent(oldText, "core.button.text"); got != "Learn more" {
		t.Fatalf("原文未变应命中旧译文，实际 %q", got)
	}

	// 原文改了一个字：hash 变 → 新 hash 无译文 → 回退新原文。
	if i18n.ContentHash(oldText) == i18n.ContentHash(newText) {
		t.Fatalf("改原文后 hash 必须变化")
	}
	newTranslator := i18n.NewContentTranslatorWith(ctx, i18n.NewDBContentStore(db), "en-US", []string{i18n.ContentHash(newText)})
	if got := newTranslator.TranslateContent(newText, "core.button.text"); got != newText {
		t.Fatalf("改原文后应回退新原文，实际 %q", got)
	}
	if newTranslator.Misses() != 1 {
		t.Fatalf("改原文后应计 1 次缺失，实际 %d", newTranslator.Misses())
	}

	// 旧行保留（D14 ②：孤儿行不清理，可追溯）。
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation WHERE source_hash = ? AND context = 'core.button.text' AND lang = 'en-US'", i18n.ContentHash(oldText)).Scan(&n).Error; err != nil {
		t.Fatalf("查询旧行失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("旧译文行应保留 1 行，实际 %d", n)
	}
}

// TestContentDefaultStoreAgainstGlobalDB 默认存储路径（全局 database 组件）：
// 批量查询命中 → 取词命中；数据库关闭后 → 查询层返回 error、取词层回退原文（不 panic）。
//
// 用独立临时库（与 i18n_seed_functional_test.go 同构），避免共享 wp_test 的历史残留。
func TestContentDefaultStoreAgainstGlobalDB(t *testing.T) {
	dbName := "go_test_content_" + randomSuffix()
	admin, err := gorm.Open(postgres.Open(i18nTestDSN("postgres")), &gorm.Config{})
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

	cfg := newI18nTestConfig(dbName)
	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	db, err := database.GetDB()
	if err != nil {
		t.Fatalf("获取数据库实例失败: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	insertTranslation(t, db, "了解更多", "core.button.text", "en-US", "Learn more", "manual")

	ctx := context.Background()
	hashes := []string{i18n.ContentHash("了解更多"), i18n.ContentHash("联系我们")}

	// 包级查询层（走全局 database 组件）。
	targets, err := i18n.LoadContentTargets(ctx, "en-US", hashes)
	if err != nil {
		t.Fatalf("包级批量查询失败: %v", err)
	}
	if got := targets[i18n.ContentIndexKey(i18n.ContentHash("了解更多"), "core.button.text")]; got != "Learn more" {
		t.Fatalf("包级查询命中错误: %q", got)
	}

	// 包级取词器（默认存储 = sys_translation 表）。
	tr := i18n.NewContentTranslator(ctx, "en-US", hashes)
	if got := tr.TranslateContent("了解更多", "core.button.text"); got != "Learn more" {
		t.Fatalf("默认存储取词错误: %q", got)
	}

	// 数据库不可用：查询层返回 error，取词层回退原文（兜底铁律）。
	if err := database.Close(); err != nil {
		t.Fatalf("关闭数据库失败: %v", err)
	}
	if _, err := i18n.LoadContentTargets(ctx, "en-US", hashes); err == nil {
		t.Fatalf("数据库未初始化时查询层应返回 error")
	}
	fallback := i18n.NewContentTranslator(ctx, "en-US", hashes)
	if got := fallback.TranslateContent("了解更多", "core.button.text"); got != "了解更多" {
		t.Fatalf("数据库不可用应回退原文，实际 %q", got)
	}
}
