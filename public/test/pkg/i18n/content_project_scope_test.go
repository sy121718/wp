package i18n_test

import (
	"testing"

	"go_wp/pkg/i18n"

	"gorm.io/gorm"
)

const (
	scopeProjectA = "11111111-1111-1111-1111-111111111111"
	scopeProjectB = "22222222-2222-2222-2222-222222222222"
	scopeProjectC = "33333333-3333-3333-3333-333333333333"
)

// newProjectScopeDB 直接用终态模板库：195 的工程作用域列已在 schema 里。
func newProjectScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newContentWriteDB(t)
}

func insertTestProject(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	if err := db.Exec("INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, ?, '{}'::jsonb, now(), now())", id, name).Error; err != nil {
		t.Fatalf("插入工程失败: %v", err)
	}
}

func countTranslationRowsForProject(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation WHERE project_id = ?::uuid", projectID).Scan(&n).Error; err != nil {
		t.Fatalf("统计工程译文行数失败: %v", err)
	}
	return n
}

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

	if got := loadOneProject(t, writer, scopeProjectA, hash); got != "A 品牌说法" {
		t.Fatalf("工程 A 应读到自己的译法，实际 %q", got)
	}
	if got := loadOneProject(t, writer, scopeProjectB, hash); got != "B 品牌说法" {
		t.Fatalf("工程 B 应读到自己的译法，实际 %q", got)
	}

	if got := loadOneProject(t, writer, scopeProjectC, hash); got != "Learn more" {
		t.Fatalf("工程 C 应回落全局译文，实际 %q", got)
	}

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

func loadOneProject(t *testing.T, writer *i18n.ContentWriter, projectID, hash string) string {
	t.Helper()
	targets, err := writer.LoadTargetsForProject(t.Context(), projectID, "en-US", []string{hash})
	if err != nil {
		t.Fatalf("按工程读取失败: %v", err)
	}
	return targets[i18n.ContentIndexKey(hash, "core.button.text")]
}
