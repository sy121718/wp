package migrations_test

// 307_build_jobs_intent_test.go — 迁移 307 的判定、幂等与「新待办键」的行为回归。
//
// 为什么值得单独钉住：判定写错的后果不是报错，而是这条迁移**每次启动都重跑**；
// 而索引写错的后果更隐蔽 —— 多语言任务被互相去重、人工构建被依赖重建吞掉，
// 两种都只在业务层表现为「某些工作永远不做」。因此这里既验判定与幂等，
// 也直接往表里插行验新键的接受 / 拒绝形状。

import (
	"testing"

	"go_wp/pkg/database"
	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// 迁移 307 的版本号（与 register_build_intent.go 一致）。
const buildJobsIntentVersion = "307-build-jobs-intent"

// TestBuildJobsIntentMigrationCheckAndIdempotent 判定命中「已应用」，且整条 SQL 可重放。
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

	// 幂等：整条 SQL 在已应用的库上原样重放不得报错（DROP + CREATE 的部分唯一索引也在这条里）。
	for _, stmt := range migrations.SplitStatements(target.SQL) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("重放失败（迁移不幂等）: %v / SQL: %s", err, stmt)
		}
	}
	assertApplied("重放后")

	// 形状抽查：新键必须正好是那五列（旧的四列键意味着多语言任务会被误去重）。
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

// TestBuildJobsPendingKeyAcceptsMultiLangAndIntent 新键按语言 / 意图分开待办，同键仍然去重。
//
// 这一条是**失败能力验证**的靶子：把索引改回 (source_type, source_id, build_input_hash)，
// 第二个语言的插入立刻撞唯一键 —— 多语言站点在队列侧只会构建一种语言。
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
	// 同一来源同一输入、另一种语言：旧的四列键会把它当重复挡下。
	mustOK("第二个语言", "en-US", "dependency")
	// 同一来源同一输入同一语言、不同意图：旧键同样会挡下（人工构建被依赖重建吞掉）。
	mustOK("手工构建", "zh-CN", "manual")

	// 完全同键的重复待办仍必须被拒绝（幂等语义不变）。
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
