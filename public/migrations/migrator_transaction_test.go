package migrations_test

import (
	"fmt"
	"testing"
	"time"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

func migrationProbeTable() string {
	return fmt.Sprintf("wp_migration_atomic_probe_%d", time.Now().UnixNano())
}

func TestApplyStatementsAtomicOnFailure(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	table := migrationProbeTable()

	parts := []string{
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
	}
	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", parts); err == nil {
		t.Fatalf("第二条语句必然失败，ApplyStatements 却返回 nil")
	}

	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", table).Scan(&exists).Error; err != nil {
		t.Fatalf("检查探针表是否存在失败：%v", err)
	}
	if exists {
		t.Fatalf("迁移中途失败后仍留下半成品对象 %s：原子性被破坏（R-01 回归）", table)
	}
}

func TestApplyStatementsCommitsOnSuccess(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	table := migrationProbeTable()
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS " + table).Error
	})

	parts := []string{
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
		"COMMENT ON TABLE " + table + " IS '迁移原子性用例探针'",
	}
	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", parts); err != nil {
		t.Fatalf("两条合法语句应当整体提交，却失败：%v", err)
	}

	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", table).Scan(&exists).Error; err != nil {
		t.Fatalf("检查探针表是否存在失败：%v", err)
	}
	if !exists {
		t.Fatalf("成功路径没有把语句落库：%s 不存在", table)
	}

	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", parts); err != nil {
		t.Fatalf("重跑已完成的迁移应当跳过而非失败：%v", err)
	}
}

func TestApplyStatementsRetryAfterFailureLeavesCompleteness(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	table := migrationProbeTable()
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS " + table).Error
	})

	broken := []string{
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
		"ALTER TABLE " + table + " ADD CONSTRAINT fk_probe FOREIGN KEY (id) REFERENCES no_such_table_probe (id)",
	}
	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", broken); err == nil {
		t.Fatalf("外键指向不存在的表必然失败，ApplyStatements 却返回 nil")
	}

	var exists bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", table).Scan(&exists).Error; err != nil {
		t.Fatalf("检查探针表是否存在失败：%v", err)
	}
	if exists {
		t.Fatalf("失败那一轮留下半成品 %s：重跑会被「对象已存在，跳过」吞掉，后续 DDL 永不执行（R-01）", table)
	}

	fixed := []string{
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
		"ALTER TABLE " + table + " ADD COLUMN note text",
	}
	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", fixed); err != nil {
		t.Fatalf("修正后重跑应当成功，却失败：%v", err)
	}

	var hasNote bool
	if err := db.Raw(
		"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = ? AND column_name = 'note')",
		table,
	).Scan(&hasNote).Error; err != nil {
		t.Fatalf("检查列是否存在失败：%v", err)
	}
	if !hasNote {
		t.Fatalf("重跑后对象不齐备：%s 少了第二条语句产出的 note 列", table)
	}
}
