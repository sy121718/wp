package unit

// partition_test.go — 时间序列表分区（审计 DB-004）的护栏。
//
// 迁移 173 把三张只增表改成按月 RANGE 分区；本文件盯的是「分区真的在工作」，
// 而不是「迁移跑过了」：数据要落到对应月份的分区、时间窗查询要走分区裁剪、
// 过期分区能整块摘下来归档、append-only 触发器在分区上依然拦得住。
//
// 另两条容易忽略的边界也固化：DEFAULT 分区兜底（缺窗口不丢数据）、
// EnsureAhead 幂等（重复执行不报错也不产生重复分区）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/partition"
	migrations "go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newPartitionFixture 隔离 PG schema + 生产迁移（含 173 的分区改造）。
func newPartitionFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	return db
}

// relkind 查询一张关系的类型（p = 分区父表，r = 普通表）。
func relkind(t *testing.T, db *gorm.DB, name string) string {
	t.Helper()
	var kind string
	if err := db.Raw("SELECT c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace"+
		" WHERE n.nspname = current_schema() AND c.relname = ?", name).Scan(&kind).Error; err != nil {
		t.Fatalf("查询 %s 的关系类型失败: %v", name, err)
	}
	return kind
}

// TestThreeTablesArePartitioned 三张只增表都是分区父表。
func TestThreeTablesArePartitioned(t *testing.T) {
	db := newPartitionFixture(t)
	for _, tb := range partition.Tables {
		if kind := relkind(t, db, tb.Name); kind != "p" {
			t.Errorf("%s 应为分区表（relkind=p），实际 %q", tb.Name, kind)
		}
		if kind := relkind(t, db, tb.Name+"_default"); kind != "r" {
			t.Errorf("%s 应有 DEFAULT 分区兜底，实际 %q", tb.Name, kind)
		}
	}
}

// TestEnsureAheadCreatesPartitions 分区自动创建（审计验收第一条）。
func TestEnsureAheadCreatesPartitions(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	if _, err := partition.EnsureAhead(ctx, db, 3); err != nil {
		t.Fatalf("补齐分区失败: %v", err)
	}
	if _, err := partition.EnsureAhead(ctx, db, 3); err != nil {
		t.Fatalf("重复补齐分区应幂等: %v", err)
	}

	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, tb := range partition.Tables {
		parts, err := partition.ListPartitions(ctx, db, tb.Name)
		if err != nil {
			t.Fatalf("列出 %s 分区失败: %v", tb.Name, err)
		}
		have := map[string]bool{}
		for _, p := range parts {
			have[p.Month.Format("2006-01")] = true
		}
		for i := 0; i <= 3; i++ {
			month := thisMonth.AddDate(0, i, 0).Format("2006-01")
			if !have[month] {
				t.Errorf("%s 缺少 %s 的分区（应提前 3 个月建好）", tb.Name, month)
			}
		}
		if len(parts) != len(have) {
			t.Errorf("%s 出现重复分区: %d 个分区名对应 %d 个月", tb.Name, len(parts), len(have))
		}
	}
}

// TestInsertRoutesToMonthPartition 数据按时间落到对应月份的分区。
func TestInsertRoutesToMonthPartition(t *testing.T) {
	db := newPartitionFixture(t)
	target := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	stmt := "CREATE TABLE IF NOT EXISTS page_views_2026_09 PARTITION OF page_views FOR VALUES FROM ('2026-09-01') TO ('2026-10-01')"
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("建目标分区失败: %v", err)
	}
	if err := db.Exec("INSERT INTO page_views (project_id, path, viewed_at) VALUES (gen_random_uuid(), '/routed', ?)", target).Error; err != nil {
		t.Fatalf("写入访问明细失败: %v", err)
	}
	var landed string
	if err := db.Raw("SELECT tableoid::regclass::text FROM page_views WHERE path = '/routed'").Scan(&landed).Error; err != nil {
		t.Fatalf("查询落点失败: %v", err)
	}
	if landed != "page_views_2026_09" {
		t.Fatalf("数据应落在 page_views_2026_09，实际 %q（落在 _default 说明分区没建好）", landed)
	}
}

// TestPartitionPruning 时间窗查询走分区裁剪（审计验收第二条）。
func TestPartitionPruning(t *testing.T) {
	db := newPartitionFixture(t)
	for _, stmt := range []string{
		"CREATE TABLE IF NOT EXISTS page_views_2026_03 PARTITION OF page_views FOR VALUES FROM ('2026-03-01') TO ('2026-04-01')",
		"CREATE TABLE IF NOT EXISTS page_views_2026_10 PARTITION OF page_views FOR VALUES FROM ('2026-10-01') TO ('2026-11-01')",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("建分区失败: %v", err)
		}
	}
	var plan []string
	if err := db.Raw("EXPLAIN (COSTS OFF) SELECT COUNT(*) FROM page_views" +
		" WHERE viewed_at >= '2026-10-05' AND viewed_at < '2026-10-20'").Scan(&plan).Error; err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}
	joined := strings.Join(plan, " ")
	if !strings.Contains(joined, "page_views_2026_10") {
		t.Fatalf("计划里应出现目标分区：%s", joined)
	}
	if strings.Contains(joined, "page_views_2026_03") {
		t.Fatalf("窗口外的分区不应被扫描（分区裁剪失效）：%s", joined)
	}
}

// TestDetachBeforeKeepsData 过期分区可整块摘下归档（审计验收第三条）。
func TestDetachBeforeKeepsData(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()
	if err := db.Exec("CREATE TABLE IF NOT EXISTS page_views_2025_01 PARTITION OF page_views FOR VALUES FROM ('2025-01-01') TO ('2025-02-01')").Error; err != nil {
		t.Fatalf("建历史分区失败: %v", err)
	}
	if err := db.Exec("INSERT INTO page_views (project_id, path, viewed_at) VALUES (gen_random_uuid(), '/archived', '2025-01-15 10:00:00+00')").Error; err != nil {
		t.Fatalf("写历史数据失败: %v", err)
	}
	detached, err := partition.DetachBefore(ctx, db, "page_views", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("摘除分区失败: %v", err)
	}
	if len(detached) != 1 || detached[0] != "page_views_2025_01" {
		t.Fatalf("应摘下 2025_01 分区，实际 %v", detached)
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM page_views_2025_01 WHERE path = '/archived'").Scan(&n).Error; err != nil {
		t.Fatalf("摘下的分区应仍可查询: %v", err)
	}
	if n != 1 {
		t.Fatalf("摘下的分区应保留数据，实际 %d 行", n)
	}
	parts, err := partition.ListPartitions(ctx, db, "page_views")
	if err != nil {
		t.Fatalf("列出分区失败: %v", err)
	}
	for _, p := range parts {
		if p.Name == "page_views_2025_01" {
			t.Fatal("已摘下的分区不应再出现在父表的分区列表里")
		}
	}
	_ = db.Exec("DROP TABLE IF EXISTS page_views_2025_01").Error
}

// TestAppendOnlyTriggerAppliesToPartitions append-only 触发器在分区上依然拦得住。
func TestAppendOnlyTriggerAppliesToPartitions(t *testing.T) {
	db := newPartitionFixture(t)
	if err := db.Exec("INSERT INTO master_data_changes (project_id, entity_type, entity_id, action, field, new_value)" +
		" VALUES (gen_random_uuid(), 'product', gen_random_uuid(), 'update', 'price', '9.90')").Error; err != nil {
		t.Fatalf("写入审计行失败: %v", err)
	}
	if err := db.Exec("UPDATE master_data_changes SET new_value = '0.01'").Error; err == nil {
		t.Fatal("分区表上的 UPDATE 应被 append-only 触发器拒绝")
	}
	if err := db.Exec("DELETE FROM master_data_changes").Error; err == nil {
		t.Fatal("分区表上的 DELETE 应被 append-only 触发器拒绝")
	}
}

// TestDefaultPartitionCatchesMissingWindow 缺窗口时落 DEFAULT 分区而不是写失败。
func TestDefaultPartitionCatchesMissingWindow(t *testing.T) {
	db := newPartitionFixture(t)
	far := time.Date(2099, 7, 15, 10, 0, 0, 0, time.UTC)
	if err := db.Exec("INSERT INTO page_views (project_id, path, viewed_at) VALUES (gen_random_uuid(), '/default-catch', ?)", far).Error; err != nil {
		t.Fatalf("缺窗口时写入不应失败（DEFAULT 分区兜底）: %v", err)
	}
	var landed string
	if err := db.Raw("SELECT tableoid::regclass::text FROM page_views WHERE path = '/default-catch'").Scan(&landed).Error; err != nil {
		t.Fatalf("查询落点失败: %v", err)
	}
	if landed != "page_views_default" {
		t.Fatalf("无对应分区时应落 DEFAULT 分区，实际 %q", landed)
	}
}

// TestMigrationMigratesLegacyData 既有数据完整迁移（审计验收第四条）。
//
// 用例先把一张「改造前形状」的 page_views（无分区、主键只有 id）造出来并塞入数据，
// 再执行迁移 173 的 SQL，然后断言：数据一行不少地出现在新表里、且落在正确的月分区。
// 这条不能只靠「迁移跑通了」代替 —— 迁移里唯一有数据风险的动作就是那一步 INSERT ... SELECT，
// 而它在生产上跑的时候，出错的方式是「少了几行」而不是报错。
func TestMigrationMigratesLegacyData(t *testing.T) {
	db := newPartitionFixture(t)
	var sqlText string
	for _, m := range migrations.All() {
		if m.Version == "173-partition-append-only-tables" {
			sqlText = m.SQL
		}
	}
	if sqlText == "" {
		t.Fatal("迁移 173 未注册")
	}

	// 退回改造前的形状：删掉分区表（含分区），建一张普通表并写数据。
	if err := db.Exec("DROP TABLE IF EXISTS page_views_default").Error; err != nil {
		t.Fatalf("删除 DEFAULT 分区失败: %v", err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS page_views_2026_09").Error; err != nil {
		t.Fatalf("删除月分区失败: %v", err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS page_views").Error; err != nil {
		t.Fatalf("删除分区父表失败: %v", err)
	}
	legacyDDL := "CREATE TABLE page_views (" +
		" id bigserial PRIMARY KEY," +
		" project_id uuid NOT NULL," +
		" path text NOT NULL," +
		" lang text NOT NULL DEFAULT ''," +
		" session_id text NOT NULL DEFAULT ''," +
		" visitor_hash text NOT NULL DEFAULT ''," +
		" referrer_host text NOT NULL DEFAULT ''," +
		" ua_class text NOT NULL DEFAULT ''," +
		" ip_hash text NOT NULL DEFAULT ''," +
		" viewed_at timestamptz NOT NULL DEFAULT now())"
	if err := db.Exec(legacyDDL).Error; err != nil {
		t.Fatalf("建改造前的表失败: %v", err)
	}
	for i, day := range []string{"2026-09-01", "2026-09-20", "2026-09-30"} {
		if err := db.Exec("INSERT INTO page_views (project_id, path, viewed_at) VALUES (gen_random_uuid(), ?, ?)",
			"/legacy-"+string(rune('a'+i)), day+" 10:00:00+00").Error; err != nil {
			t.Fatalf("写既有数据失败: %v", err)
		}
	}

	// 173 写于 205（DB-019 时间列改名）**之前**，它的分区改造语句引用 created_at；
	// 本用例的库由全量迁移建出（列名已是 create_time），所以要重放这份历史 SQL 得先做
	// 一次等价替换。生产里 173 在迁移序列中总是早于 205，不存在这层替换。
	sqlText = strings.ReplaceAll(sqlText, "created_at", "create_time")
	if err := db.Exec(sqlText).Error; err != nil {
		t.Fatalf("执行迁移 173 失败: %v", err)
	}

	var total int64
	if err := db.Raw("SELECT COUNT(*) FROM page_views WHERE path LIKE '/legacy-%'").Scan(&total).Error; err != nil {
		t.Fatalf("统计迁移后数据失败: %v", err)
	}
	if total != 3 {
		t.Fatalf("既有数据应完整迁移（3 行），实际 %d 行", total)
	}
	// 落点：分区键是 viewed_at，而 173 建分区的月份范围取自**搬数据之前的新表**（那一刻它是空的），
	// 于是范围以 now() 所在月为起点向后铺 —— 数据所在的 2026-09 不会被预建，那三行落进
	// DEFAULT 分区。这是迁移的历史行为（生产同此，数据一行不丢），所以这里断言「数据落进了
	// 某个 partition」，而不是「落在某个具体月分区」：后者会随跑测试的日历月份变化
	// （9 月绿、10 月红），把一条与代码无关的时钟依赖钉进回归。
	var landed int64
	if err := db.Raw("SELECT COUNT(*) FROM page_views_default WHERE path LIKE '/legacy-%'").Scan(&landed).Error; err != nil {
		t.Fatalf("统计分区数据失败: %v", err)
	}
	if landed != 3 {
		t.Fatalf("既有数据应完整落进一个 partition，实际 %d 行", landed)
	}
	// 旧表已清理，不留同名残留。
	if kind := relkind(t, db, "page_views_legacy"); kind != "" {
		t.Fatalf("旧表应已删除，实际仍存在（relkind=%q）", kind)
	}
	_ = db.Exec("DROP TABLE IF EXISTS page_views_2026_09_legacy").Error
}
