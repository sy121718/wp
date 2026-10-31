package migrations_test

import (
	"testing"

	"go_wp/pkg/database"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

const buildJobsIntentVersion = "307-build-jobs-intent"

func TestBuildJobsIntentMigrationCheckAndIdempotent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	var target migrations.Migration
	for _, m := range migrations.All() {
		if m.Version == buildJobsIntentVersion {
			target = m
			break
		}
	}
	if target.Version == "" {
		t.Fatalf("迁移 %s 未注册", buildJobsIntentVersion)
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

	var cols []string
	if err := db.Raw(`SELECT a.attname FROM pg_index x
		JOIN pg_class c ON c.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class ic ON ic.oid = x.indexrelid
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (x.indkey)
		WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
		  AND ic.relname = 'uq_build_jobs_pending' AND x.indisunique AND x.indpred IS NOT NULL
		ORDER BY a.attnum`).Scan(&cols).Error; err != nil {
		t.Fatalf("读取索引键列失败: %v", err)
	}
	want := []string{"source_type", "source_id", "build_input_hash", "lang", "intent"}
	if len(cols) != len(want) {
		t.Fatalf("uq_build_jobs_pending 的键列应为 %v，实际 %v", want, cols)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Fatalf("uq_build_jobs_pending 的键列应为 %v，实际 %v", want, cols)
		}
	}
}

func TestBuildJobsPendingKeyAcceptsMultiLangAndIntent(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	const insert = `INSERT INTO build_jobs
		(source_type, source_id, build_input_hash, lang, intent, draft_version, status, create_time)
		VALUES ('page', ?, ?, ?, ?, 1, 'pending', now())`
	src := "11111111-1111-4111-8111-111111111111"

	mustOK := func(stage, lang, intent string) {
		t.Helper()
		if err := db.Exec(insert, src, "h1", lang, intent).Error; err != nil {
			t.Fatalf("%s：入队 %s/%s 失败: %v", stage, lang, intent, err)
		}
	}
	mustOK("首个语言", "zh-CN", "dependency")

	mustOK("第二个语言", "en-US", "dependency")

	mustOK("手工构建", "zh-CN", "manual")

	err := db.Exec(insert, src, "h1", "zh-CN", "dependency").Error
	if err == nil {
		t.Fatal("同来源同输入同语言同意图的重复待办必须被 uq_build_jobs_pending 拒绝")
	}
	if !database.IsUniqueViolation(err) {
		t.Fatalf("应撞唯一约束，实际: %v", err)
	}

	var n int64
	if err := db.Table("build_jobs").Where("source_id = ?", src).Count(&n).Error; err != nil {
		t.Fatalf("统计任务失败: %v", err)
	}
	if n != 3 {
		t.Fatalf("应有 3 条互不相同的待办（两语言 + 手工/依赖），实际 %d", n)
	}
}
