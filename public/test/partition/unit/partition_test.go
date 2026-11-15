package unit

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/partition"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

func newPartitionFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	return db
}

func relkind(t *testing.T, db *gorm.DB, name string) string {
	t.Helper()
	var kind string
	if err := db.Raw("SELECT c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace"+
		" WHERE n.nspname = current_schema() AND c.relname = ?", name).Scan(&kind).Error; err != nil {
		t.Fatalf("查询 %s 的关系类型失败: %v", name, err)
	}
	return kind
}

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
