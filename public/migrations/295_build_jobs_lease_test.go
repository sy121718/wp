package migrations_test

import (
	"testing"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const buildJobsLeaseVersion = "295-build-jobs-lease"

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

	for _, stmt := range migrations.SplitStatements(target.SQL) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放失败（迁移不幂等）: %v / SQL: %s", err, stmt)
		}
	}
	assertApplied("重放后")

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
