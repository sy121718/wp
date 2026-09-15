package i18n_test

// content_project_scope_test.go — sys_translation 工程作用域（审计 I18N-009）在真实
// PostgreSQL 上的链路验证。
//
// 迁移 195 由本文件手工应用：public/migrations/register.go 由父代理统一追加注册，
// 本批不改它；手工执行与注册后的自动执行是幂等的（ADD COLUMN IF NOT EXISTS /
// DROP CONSTRAINT IF EXISTS / CREATE INDEX IF NOT EXISTS），因此两种状态下测试都成立。
//
// 覆盖：
//  1. 同 key 译文在各工程间独立（A 站 / B 站各写各的，互不覆盖）；
//  2. 未覆盖的工程回落全局行；
//  3. 无工程上下文的读取（离线构建路径）取全局行，且同一键只回一行；
//  4. 写入幂等：同工程同 key 重复保存仍是一行，不影响其它工程。
//
// PG 不可用时 t.Skip（与其他功能测试一致）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/pkg/i18n"
	"go_wp/public/migrations"

	"gorm.io/gorm"
)

// 测试用工程 id（固定值，便于失败时与 SQL 输出对照）。
const (
	scopeProjectA = "11111111-1111-1111-1111-111111111111"
	scopeProjectB = "22222222-2222-2222-2222-222222222222"
	scopeProjectC = "33333333-3333-3333-3333-333333333333"
)

// newProjectScopeDB 建隔离 schema、跑全量迁移，再手工应用 195（工程作用域列）。
func newProjectScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newContentWriteDB(t)
	applyMigrationFile(t, db, "195_sys_translation_project_scope.sql")
	return db
}

// applyMigrationFile 执行单个迁移文件（按语句边界拆分后逐条执行）。
func applyMigrationFile(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "migrations", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取迁移文件失败（%s）: %v", path, err)
	}
	for _, stmt := range migrations.SplitStatements(string(raw)) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行迁移 %s 失败: %v\nSQL: %s", name, err, stmt)
		}
	}
}

// insertTestProject 插入一个工程（sys_translation 的工程外键需要真实工程行）。
func insertTestProject(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	if err := db.Exec("INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, ?, '{}'::jsonb, now(), now())", id, name).Error; err != nil {
		t.Fatalf("插入工程失败: %v", err)
	}
}

// countTranslationRowsForProject 统计某工程的工程级译文行数。
func countTranslationRowsForProject(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation WHERE project_id = ?::uuid", projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计工程译文行数失败: %v", err)
	}
	return n
}

// TestContentTranslationProjectScope 同 key 译文按工程隔离，未覆盖时回落全局。
func TestContentTranslationProjectScope(t *testing.T) {
	db := newProjectScopeDB(t)
	insertTestProject(t, db, scopeProjectA, "工程A")
	insertTestProject(t, db, scopeProjectB, "工程B")
	insertTestProject(t, db, scopeProjectC, "工程C（未翻译）")
	ctx := t.Context()
	writer := i18n.NewContentWriter(db)

	src := "了解更多"
	hash := i18n.ContentHash(src)
	key := i18n.ContentIndexKey(hash, "core.button.text")
	items := []i18n.ContentWriteItem{
		{SourceHash: hash, Context: "core.button.text", Lang: "en-US", SourceText: src, TargetText: "Learn more"},
		{ProjectID: scopeProjectA, SourceHash: hash, Context: "core.button.text", Lang: "en-US", SourceText: src, TargetText: "A 品牌说法"},
		{ProjectID: scopeProjectB, SourceHash: hash, Context: "core.button.text", Lang: "en-US", SourceText: src, TargetText: "B 品牌说法"},
	}
	if written, err := writer.Upsert(ctx, items); err != nil || written != 3 {
		t.Fatalf("写入三条作用域行失败: written=%d err=%v", written, err)
	}

	// 1) 工程 A / B 各读各的（同 key 不同译法互不影响）。
	if got := loadOneProject(t, writer, scopeProjectA, hash); got != "A 品牌说法" {
		t.Fatalf("工程 A 应读到自己的译法，实际 %q", got)
	}
	if got := loadOneProject(t, writer, scopeProjectB, hash); got != "B 品牌说法" {
		t.Fatalf("工程 B 应读到自己的译法，实际 %q", got)
	}

	// 2) 工程 C 没有覆盖行 → 回落全局行。
	if got := loadOneProject(t, writer, scopeProjectC, hash); got != "Learn more" {
		t.Fatalf("工程 C 应回落全局译文，实际 %q", got)
	}

	// 3) 无工程上下文（离线构建路径）：取全局行，且同一键只回一行。
	targets, err := writer.LoadTargets(ctx, "en-US", []string{hash})
	if err != nil {
		t.Fatalf("无工程上下文读取失败: %v", err)
	}
	if got := targets[key]; got != "Learn more" {
		t.Fatalf("无工程上下文应取全局译文（同键多行时结果必须确定），实际 %q", got)
	}
	if len(targets) != 1 {
		t.Fatalf("同一 (hash, context) 应只回一行，实际 %d 行", len(targets))
	}

	// 4) 幂等：同工程同 key 再保存一次仍是一行，且不动其它工程。
	beforeA := countTranslationRowsForProject(t, db, scopeProjectA)
	if _, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		{ProjectID: scopeProjectA, SourceHash: hash, Context: "core.button.text", Lang: "en-US", SourceText: src, TargetText: "A 品牌说法 v2"},
	}); err != nil {
		t.Fatalf("重复保存失败: %v", err)
	}
	if afterA := countTranslationRowsForProject(t, db, scopeProjectA); afterA != beforeA {
		t.Fatalf("重复保存不应新增行: %d -> %d", beforeA, afterA)
	}
	if got := loadOneProject(t, writer, scopeProjectA, hash); got != "A 品牌说法 v2" {
		t.Fatalf("重复保存后应更新为 v2，实际 %q", got)
	}
	if got := loadOneProject(t, writer, scopeProjectB, hash); got != "B 品牌说法" {
		t.Fatalf("工程 A 的重复保存影响了工程 B，实际 %q", got)
	}
	if total := countTranslationRows(t, db); total != 3 {
		t.Fatalf("三个作用域应各一行（全局 + A + B），实际 %d 行", total)
	}

	// 5) 工程删除时联动清掉它的工程级译文（外键 ON DELETE CASCADE）。
	if err := db.Exec("DELETE FROM projects WHERE id = ?::uuid", scopeProjectB).Error; err != nil {
		t.Fatalf("删除工程失败: %v", err)
	}
	if left := countTranslationRowsForProject(t, db, scopeProjectB); left != 0 {
		t.Fatalf("工程删除后其译文应被级联清除，实际剩 %d 行", left)
	}
	if total := countTranslationRows(t, db); total != 2 {
		t.Fatalf("级联删除不应影响其它作用域（应剩 2 行），实际 %d 行", total)
	}
}

// loadOneProject 读某工程在该原文上的译文（取不到返回空串）。
func loadOneProject(t *testing.T, writer *i18n.ContentWriter, projectID, hash string) string {
	t.Helper()
	targets, err := writer.LoadTargetsForProject(t.Context(), projectID, "en-US", []string{hash})
	if err != nil {
		t.Fatalf("按工程读取失败: %v", err)
	}
	return targets[i18n.ContentIndexKey(hash, "core.button.text")]
}
