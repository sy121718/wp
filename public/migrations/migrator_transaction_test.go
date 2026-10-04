package migrations_test

// migrator_transaction_test.go — 迁移的原子性回归（审计 R-01）。
//
// 判据：一条迁移里的多条语句要么全生效、要么全不生效。此前 apply 是逐条 db.Exec 且
// 没有外层事务，中途失败会留下**半成品对象**；而重跑时存在性检查只看主对象是否已建 →
// 打印「迁移对象已存在，跳过」直接返回 nil，剩下的 DDL 永远不再执行且没有告警。
// 全仓 381 个迁移里 262 个是多语句，触发面不是边角。
//
// 这两条用例钉住的就是那件事：失败路径不留半成品，成功路径不被误回滚。

import (
	"fmt"
	"testing"
	"time"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// migrationProbeTable 生成探针表名：模板库是复用的，名字必须每次唯一，
// 否则并发跑的两次用例会互相撞上「表已存在」。
func migrationProbeTable() string {
	return fmt.Sprintf("wp_migration_atomic_probe_%d", time.Now().UnixNano())
}

func TestApplyStatementsAtomicOnFailure(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	table := migrationProbeTable()

	// 第二条必然失败（与第一条同名建表）。
	parts := []string{
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
		"CREATE TABLE " + table + " (id int PRIMARY KEY)",
	}
	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", parts); err == nil {
		t.Fatalf("第二条语句必然失败，ApplyStatements 却返回 nil")
	}

	// 关键断言：第一条语句也不能留下。留下 = R-01 复现（重跑会被「已存在，跳过」吞掉）。
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

	// 幂等：重跑同一条迁移应识别「对象已存在」并跳过（CheckSQL 默认判定主对象）。
	if err := migrations.ApplyStatements(db, "9999-atomic-probe", table, "", parts); err != nil {
		t.Fatalf("重跑已完成的迁移应当跳过而非失败：%v", err)
	}
}

// TestApplyStatementsRetryAfterFailureLeavesCompleteness 对准 R-01 的真实运维路径：
// 一条多语句迁移在线上跑到一半失败（此处用「外键指向不存在的表」复现），DBA 改完语句重跑。
// 判据是两层：失败那一轮不能留下任何对象（否则重跑会撞上「迁移对象已存在，跳过」，
// 后面的 DDL 永远不再执行）；修正后那一轮必须**对象齐备** —— 不是「主表在就算过」，
// 而是每条语句的产物都在。
func TestApplyStatementsRetryAfterFailureLeavesCompleteness(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	table := migrationProbeTable()
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE IF EXISTS " + table).Error
	})

	// 第一轮：建表成功、第二条语句必然失败（外键指向不存在的表）。
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

	// 第二轮：改成合法语句重跑，存在性检查必须不早退（上一轮没留下对象），两条都要生效。
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
