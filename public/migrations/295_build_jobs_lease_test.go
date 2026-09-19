package migrations_test

// 295_build_jobs_lease_test.go — 迁移 295 的判定与幂等回归。
//
// 为什么值得单独钉住：AGENTS 记录的判定坑是「CheckSQL 里的 ? 是表名，其它值必须写进 SQL 字面量」——
// 判定写错的后果不是报错，而是这条迁移**每次启动都重跑**（178 踩过，被 SQL 自带的幂等性掩盖）。
// 另外这里也验证整条 SQL 可以在已应用的库上原样重放：迁移必须幂等，
// 否则「判定一旦失效」就会演变成启动失败。

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 迁移 295 的版本号（与 register_build_lease.go 一致）。
const buildJobsLeaseVersion = "295-build-jobs-lease"

// TestBuildJobsLeaseMigrationCheckAndIdempotent 判定命中「已应用」，且整条 SQL 可重放。
func TestBuildJobsLeaseMigrationCheckAndIdempotent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	var target migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == buildJobsLeaseVersion {
			target = m
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("迁移 %s 未注册", buildJobsLeaseVersion)
	}

	// 判定：迁移跑过之后必须返回 1（六列 + 来源互斥索引都在位）。
	assertApplied := func(stage string) {
		t.Helper()
		var n int64
		if err := db.Raw(target.CheckSQL, target.TableName).Scan(&n).Error; err != nil {
			t.Fatalf("%s：执行 CheckSQL 失败: %v", stage, err)
		}
		if n != 1 {
			t.Fatalf("%s：迁移 %s 的判定应为「已应用」，实际 %d —— 判定写错会让它每次启动都重跑",
				stage, target.Version, n)
		}
	}
	assertApplied("首次")

	// 幂等：整条 SQL 在已应用的库上原样重放不得报错。
	for _, stmt := range migrations.SplitStatements(target.SQL) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放失败（迁移不幂等）: %v / SQL: %s", err, stmt)
		}
	}
	assertApplied("重放后")

	// 形状抽查：列与索引确实在位（判定之外再确认一次结果本身）。
	var cols int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'build_jobs'
		  AND column_name IN ('project_id','lang','intent','lease_token','lease_expires_time','attempt')`).Scan(&cols).Error; err != nil {
		t.Fatalf("统计列失败: %v", err)
	}
	if cols != 6 {
		t.Fatalf("迁移 295 的六列应全部在位，实际 %d", cols)
	}
	var idx int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'build_jobs'
		  AND indexname = 'uq_build_jobs_running_source'`).Scan(&idx).Error; err != nil {
		t.Fatalf("统计索引失败: %v", err)
	}
	if idx != 1 {
		t.Fatalf("来源互斥索引 uq_build_jobs_running_source 应在位，实际 %d", idx)
	}
}
