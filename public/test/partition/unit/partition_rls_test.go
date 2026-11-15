package unit

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	pkgpartition "go_wp/pkg/database/partition"
	"go_wp/pkg/sqlsplit"
	"go_wp/public/schema"
)

func runReconcileSQL(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range sqlsplit.Split(schema.PartitionOptions().ReconcileRLSSQL()) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行 reconcile_rls.sql 失败: %v", err)
		}
	}
}

func rlsFlags(t *testing.T, db *gorm.DB, table string) (enabled, forced, hasPolicy bool) {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace"+
		" WHERE n.nspname = current_schema() AND c.relname = ?", table).Scan(&n).Error; err != nil {
		t.Fatalf("查询 %s 失败: %v", table, err)
	}
	if n == 0 {
		t.Fatalf("表 %s 不存在", table)
	}
	if err := db.Raw("SELECT c.relrowsecurity, c.relforcerowsecurity FROM pg_class c"+
		" JOIN pg_namespace n ON n.oid = c.relnamespace"+
		" WHERE n.nspname = current_schema() AND c.relname = ?", table).
		Row().Scan(&enabled, &forced); err != nil {
		t.Fatalf("读取 %s 的 RLS 开关失败: %v", table, err)
	}
	var pol int64
	if err := db.Raw("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid"+
		" JOIN pg_namespace n ON n.oid = c.relnamespace"+
		" WHERE n.nspname = current_schema() AND c.relname = ?", table).Scan(&pol).Error; err != nil {
		t.Fatalf("读取 %s 的策略失败: %v", table, err)
	}
	return enabled, forced, pol > 0
}

func gapTables(gaps []pkgpartition.RLSGap) map[string]string {
	out := make(map[string]string, len(gaps))
	for _, g := range gaps {
		out[g.Table] = g.Missing
	}
	return out
}

func TestAuditPartitionRLSCoverage(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	gaps, checked, err := pkgpartition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if checked == 0 {
		t.Fatal("受检分区数为 0：巡检判据没命中任何分区，后面的「无缺口」不能算证据")
	}
	if len(gaps) != 0 {
		t.Fatalf("迁移后仍有 %d 处分区隔离缺口：%v", len(gaps), gaps)
	}
	t.Logf("稳态受检分区 %d 个，缺口 0 处", checked)

	if err := db.Exec("CREATE TABLE IF NOT EXISTS page_views_2030_01 PARTITION OF page_views" +
		" FOR VALUES FROM ('2030-01-01') TO ('2030-02-01')").Error; err != nil {
		t.Fatalf("构造越界分区失败: %v", err)
	}
	gaps, _, err = pkgpartition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	missing, found := gapTables(gaps)["page_views_2030_01"]
	if !found {
		t.Fatalf("巡检没有发现人为构造的缺口（这是红绿判据的「红」侧，必须命中）：%v", gaps)
	}
	if !strings.Contains(missing, "enable") || !strings.Contains(missing, "policy") {
		t.Errorf("缺口描述不完整：%q，期望同时含 enable 与 policy", missing)
	}
	t.Logf("构造缺口后巡检命中：page_views_2030_01 缺 [%s]", missing)

	runReconcileSQL(t, db)
	gaps, _, err = pkgpartition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("reconcile_rls.sql 执行后仍有缺口：%v", gaps)
	}
	if en, fo, pol := rlsFlags(t, db, "page_views_2030_01"); !en || !fo || !pol {
		t.Fatalf("reconcile_rls.sql 执行后 page_views_2030_01 的 RLS 仍不完整：enable=%v force=%v policy=%v", en, fo, pol)
	}

	runReconcileSQL(t, db)
	gaps, _, err = pkgpartition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("reconcile_rls.sql 重复执行后出现缺口（幂等性破坏）：%v", gaps)
	}

	assertPredicateEqualsParent(t, db, "page_views", "page_views_2030_01")
}

func assertPredicateEqualsParent(t *testing.T, db *gorm.DB, parent, child string) {
	t.Helper()
	type polRow struct {
		PolName string `gorm:"column:polname"`
		PolCmd  string `gorm:"column:polcmd"`
		Perm    bool   `gorm:"column:polpermissive"`
		Roles   string `gorm:"column:polroles"`
		Qual    string `gorm:"column:qual"`
		WCheck  string `gorm:"column:wcheck"`
	}
	load := func(table string) map[string]polRow {
		var rows []polRow
		if err := db.Raw(`SELECT p.polname, p.polcmd::text AS polcmd, p.polpermissive, p.polroles::text AS polroles,
				COALESCE(pg_get_expr(p.polqual, p.polrelid), '') AS qual,
				COALESCE(pg_get_expr(p.polwithcheck, p.polrelid), '') AS wcheck
			FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = ?`, table).Scan(&rows).Error; err != nil {
			t.Fatalf("读取 %s 的策略失败: %v", table, err)
		}
		out := make(map[string]polRow, len(rows))
		for _, r := range rows {
			out[r.PolName] = r
		}
		return out
	}
	parentPols := load(parent)
	childPols := load(child)
	if len(parentPols) == 0 {
		t.Fatalf("父表 %s 没有任何策略，断言无从谈起", parent)
	}
	for name, pp := range parentPols {
		cp, ok := childPols[name]
		if !ok {
			t.Errorf("分区 %s 缺少父表 %s 的策略 %s", child, parent, name)
			continue
		}
		if cp.Qual != pp.Qual || cp.WCheck != pp.WCheck {
			t.Errorf("策略 %s 的谓词与父表不一致：\n  父表 qual=%q wcheck=%q\n  分区 qual=%q wcheck=%q",
				name, pp.Qual, pp.WCheck, cp.Qual, cp.WCheck)
		}
		if cp.PolCmd != pp.PolCmd || cp.Perm != pp.Perm || cp.Roles != pp.Roles {
			t.Errorf("策略 %s 的形状与父表不一致：cmd %s/%s permissive %v/%v roles %s/%s",
				name, pp.PolCmd, cp.PolCmd, pp.Perm, cp.Perm, pp.Roles, cp.Roles)
		}
	}
}

func TestReconcileRLSCopiesArbitraryPolicyShapes(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	if err := db.Exec("DROP TABLE IF EXISTS dbtest_part").Error; err != nil {
		t.Fatalf("清理残留失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP TABLE IF EXISTS dbtest_part").Error })

	steps := []string{
		"CREATE TABLE dbtest_part (id bigint, project_id uuid NOT NULL, created_at timestamptz NOT NULL)" +
			" PARTITION BY RANGE (created_at)",
		"CREATE TABLE dbtest_part_1 PARTITION OF dbtest_part" +
			" FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')",

		"CREATE POLICY p_dbtest_a ON dbtest_part USING" +
			" (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)",

		"CREATE POLICY p_dbtest_b ON dbtest_part FOR INSERT WITH CHECK (project_id IS NOT NULL)",
	}
	for _, s := range steps {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("准备临时分区表失败（%s）: %v", s, err)
		}
	}

	if _, _, pol := rlsFlags(t, db, "dbtest_part_1"); pol {
		t.Fatal("夹具不成立：新建分区本该没有策略")
	}

	runReconcileSQL(t, db)

	enabled, forced, hasPolicy := rlsFlags(t, db, "dbtest_part_1")
	if !enabled || !forced || !hasPolicy {
		t.Fatalf("reconcile_rls.sql 没有为临时分区补隔离：enable=%v force=%v policy=%v", enabled, forced, hasPolicy)
	}

	assertPredicateEqualsParent(t, db, "dbtest_part", "dbtest_part_1")

	var n int64
	if err := db.Raw("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid" +
		" WHERE c.relname = 'dbtest_part_1'").Scan(&n).Error; err != nil {
		t.Fatalf("统计分区策略失败: %v", err)
	}
	if n != 2 {
		t.Errorf("分区上的策略数 = %d，期望 2（父表两条都要复制）", n)
	}

	runReconcileSQL(t, db)
	var again int64
	if err := db.Raw("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid" +
		" WHERE c.relname = 'dbtest_part_1'").Scan(&again).Error; err != nil {
		t.Fatalf("统计分区策略失败: %v", err)
	}
	if again != 2 {
		t.Errorf("重复执行 reconcile_rls.sql 后策略数 = %d，期望仍为 2", again)
	}
	_ = ctx
}

func TestEnsureAheadRestoresDefaultPartitionRLS(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	const def = "page_views_default"

	if en, fo, pol := rlsFlags(t, db, def); !en || !fo || !pol {
		t.Fatalf("夹具不成立：迁移建出的 %s 隔离不完整（enable=%v force=%v policy=%v）", def, en, fo, pol)
	}

	if err := db.Exec("DROP TABLE " + def).Error; err != nil {
		t.Fatalf("删除 %s 失败: %v", def, err)
	}
	if kind := relkind(t, db, def); kind != "" {
		t.Fatalf("%s 应已被删除，实际 relkind=%q", def, kind)
	}

	if _, err := pkgpartition.EnsureAhead(ctx, db, schema.PartitionOptions(), 3); err != nil {
		t.Fatalf("EnsureAhead 失败: %v", err)
	}

	enabled, forced, hasPolicy := rlsFlags(t, db, def)
	if !enabled || !forced || !hasPolicy {
		t.Fatalf("EnsureAhead 重建的 %s 没有完整的工程隔离：enable=%v force=%v policy=%v ——"+
			"兜底桶裸奔比没有兜底桶更危险（前者静默、后者丢数据会报错）", def, enabled, forced, hasPolicy)
	}
	assertPredicateEqualsParent(t, db, "page_views", def)

	gaps, _, err := pkgpartition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("重建 DEFAULT 分区后巡检出现缺口：%v", gaps)
	}
}

func TestAuditRLSCoverageWhitelist(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	raw, checked, err := pkgpartition.AuditRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if checked == 0 {
		t.Fatal("受检表数为 0：判据没命中任何表，后面的结论不能算证据")
	}
	rawGaps := gapTables(raw)
	for _, name := range []string{"build_jobs", "product_outbox_events"} {
		if _, ok := rawGaps[name]; !ok {
			t.Errorf("不带豁免时巡检未报出 %s：巡检没有真正在工作（或该表的 project_id 已不存在）", name)
		}
	}

	gaps, _, err := pkgpartition.AuditRLSCoverage(ctx, db, "build_jobs", "product_outbox_events")
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("豁免名单之外仍有 %d 处 RLS 缺口：%v", len(gaps), gaps)
	}
	t.Logf("受检 %d 张带 project_id 的基表，豁免 2 张后缺口 0 处", checked)
}

func TestEnsureAheadReconcilesArbitraryPolicyShapes(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	if err := db.Exec("DROP TABLE IF EXISTS dbtest_ahead").Error; err != nil {
		t.Fatalf("清理残留失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP TABLE IF EXISTS dbtest_ahead").Error })

	steps := []string{
		"CREATE TABLE dbtest_ahead (id bigint, project_id uuid NOT NULL, created_at timestamptz NOT NULL)" +
			" PARTITION BY RANGE (created_at)",

		"CREATE TABLE dbtest_ahead_1 PARTITION OF dbtest_ahead" +
			" FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')",

		"CREATE POLICY p_dbtest_other ON dbtest_ahead USING" +
			" (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)",
	}
	for _, s := range steps {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("准备临时分区表失败（%s）: %v", s, err)
		}
	}
	if _, _, pol := rlsFlags(t, db, "dbtest_ahead_1"); pol {
		t.Fatal("夹具不成立：临时分区本该没有策略")
	}

	if _, err := pkgpartition.EnsureAhead(ctx, db, schema.PartitionOptions(), 3); err != nil {
		t.Fatalf("EnsureAhead 失败: %v", err)
	}

	enabled, forced, hasPolicy := rlsFlags(t, db, "dbtest_ahead_1")
	if !enabled || !forced || !hasPolicy {
		t.Fatalf("EnsureAhead 之后临时分区仍无隔离：enable=%v force=%v policy=%v"+
			"（EnsureAhead 与reconcile_rls.sql 的判据不同源）", enabled, forced, hasPolicy)
	}
	assertPredicateEqualsParent(t, db, "dbtest_ahead", "dbtest_ahead_1")

	var extra int64
	if err := db.Raw("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid" +
		" WHERE c.relname = 'dbtest_ahead_1' AND p.polname NOT IN" +
		" (SELECT polname FROM pg_policy pol JOIN pg_class pc ON pc.oid = pol.polrelid WHERE pc.relname = 'dbtest_ahead')").
		Scan(&extra).Error; err != nil {
		t.Fatalf("统计多余策略失败: %v", err)
	}
	if extra != 0 {
		t.Errorf("分区上出现了父表没有的 %d 条策略（凭空加策略 = 放宽隔离）", extra)
	}
}

func TestTableTimeColumnsMatchPartitionKeys(t *testing.T) {
	db := newPartitionFixture(t)

	for _, tb := range schema.PartitionOptions().Tables {
		var partkey string
		if err := db.Raw("SELECT pg_get_partkeydef(c.oid) FROM pg_class c"+
			" JOIN pg_namespace n ON n.oid = c.relnamespace"+
			" WHERE n.nspname = current_schema() AND c.relname = ?", tb.Name).Scan(&partkey).Error; err != nil {
			t.Fatalf("读取 %s 的分区键失败: %v", tb.Name, err)
		}
		if partkey == "" {
			t.Fatalf("%s 不是分区表（夹具不成立）", tb.Name)
		}

		if !strings.Contains(partkey, "("+tb.TimeColumn+")") {
			t.Errorf("%s.TimeColumn = %q，但库里真实的分区键是 %q ——"+
				"声明的分区键与真值不符（该字段无消费方，不会在别处报错）",
				tb.Name, tb.TimeColumn, partkey)
		}
		t.Logf("%s 分区键 = %s（声明 %s）", tb.Name, partkey, tb.TimeColumn)
	}
}
