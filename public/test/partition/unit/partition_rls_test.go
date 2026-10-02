package unit

// partition_rls_test.go — 分区子表的工程隔离覆盖（审计 DB-02）的护栏。
//
// 缺陷形态：迁移 215 用**静态分区名数组**加策略，而迁移 173 / EnsureAhead 按部署时间
// **动态**建分区 —— 部署月落在 215 名单之外的库，那些分区从来没有策略；
// 而 PG 的 ENABLE / FORCE ROW LEVEL SECURITY 不递归到分区。
//
// 本文件盯四件事，每一件都有对应的失败方式：
//   1. 巡检能**发现**缺口（否则「修好了」只能靠读代码相信）；
//   2. 迁移 507 能**修**缺口，且**不假定策略名**（全库存在 4 个策略名，按名字过滤会误判）；
//   3. EnsureAhead 重建 DEFAULT 分区时**不留下没有策略的兜底桶** —— 这是本批次发现的
//      第二个缺口：建表走 CREATE TABLE IF NOT EXISTS，稳态不重建，所以缺口平时看不出来，
//      但一旦该分区被手工删掉，重建出来的那个恰好是数据兜底落点；
//   4. 迁移 506 补的索引**真的被用上且满足排序**（不是「DDL 跑过了」就算数）。

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/internal/partition"
)

// reconcileSQLPath 迁移 507 的磁盘原文（相对本测试包目录 = public/test/partition/unit）。
//
// 直接读**文件**而不是复用 Go 里的常量：迁移的真源就是那个 .sql，测试要验证的正是
// 「这份文件能在真库上跑通且幂等」。写死 SQL 常量会让测试与迁移文件悄悄分叉。
const reconcileSQLPath = "../../../migrations/507_partition_rls_reconcile.sql"

// runSQLFile 读取并执行一个迁移 SQL 文件（整段交给 PG：注释 + 单条语句）。
func runSQLFile(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	if err := db.Exec(string(raw)).Error; err != nil {
		t.Fatalf("执行 %s 失败: %v", path, err)
	}
}

// rlsFlags 读一张表的 RLS 开关与「有没有策略」。
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

// gapTables 取巡检结果里的表名集合。
func gapTables(gaps []partition.RLSGap) map[string]string {
	out := make(map[string]string, len(gaps))
	for _, g := range gaps {
		out[g.Table] = g.Missing
	}
	return out
}

// TestAuditPartitionRLSCoverage 巡检能发现缺口、507 能修、重复执行幂等。
//
// 这是 DB-02 的主验收：迁移跑完的稳态应当无缺口 —— 若这里一开始就有缺口，
// 说明 173 建出的分区没有被 215 / 507 覆盖（本仓当前的部署月恰好落在 215 名单内，
// 所以这条断言在现有环境是绿的；换一个部署月的库就会红，那正是缺陷本身）。
func TestAuditPartitionRLSCoverage(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	// ① 稳态：迁移（含 507）跑完后不应有缺口。
	gaps, checked, err := partition.AuditPartitionRLSCoverage(ctx, db)
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

	// ② 造一个「越界月分区」：215 的静态名单里没有它、EnsureAhead 也不会碰它
	// （它只回补 [上月, +3]，永不回头补历史，也不会管更远的将来）——
	// 这正是「部署月不在名单内」时 173 建出来的分区的等价物。
	if err := db.Exec("CREATE TABLE IF NOT EXISTS page_views_2030_01 PARTITION OF page_views" +
		" FOR VALUES FROM ('2030-01-01') TO ('2030-02-01')").Error; err != nil {
		t.Fatalf("构造越界分区失败: %v", err)
	}
	gaps, _, err = partition.AuditPartitionRLSCoverage(ctx, db)
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

	// ③ 跑迁移 507 的磁盘原文 → 缺口消失（绿）。
	runSQLFile(t, db, reconcileSQLPath)
	gaps, _, err = partition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("507 执行后仍有缺口：%v", gaps)
	}
	if en, fo, pol := rlsFlags(t, db, "page_views_2030_01"); !en || !fo || !pol {
		t.Fatalf("507 执行后 page_views_2030_01 的 RLS 仍不完整：enable=%v force=%v policy=%v", en, fo, pol)
	}

	// ④ 再跑一次 → 仍无缺口（幂等；507 的快速路径在这个状态下不应产生任何 DDL）。
	runSQLFile(t, db, reconcileSQLPath)
	gaps, _, err = partition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("507 重复执行后出现缺口（幂等性破坏）：%v", gaps)
	}

	// ⑤ 子表的策略表达式必须与父表**逐字相同**（否则「复制」退化成「另写一份」）。
	assertPredicateEqualsParent(t, db, "page_views", "page_views_2030_01")
}

// assertPredicateEqualsParent 断言分区上的每条策略都与父表同名策略的谓词逐字一致。
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

// TestMigration507CopiesArbitraryPolicyShapes 507 不假定策略名，也不假定策略形态。
//
// 判据来源（实测真库）：全库有 4 个策略名 —— `project_isolation`（迁移 215 铺开的 40+ 张表）
// 与 `p_comments_project` / `p_membership_tiers_project` / `p_membership_assignments_project`
// （462/466 系列自建）。**按策略名过滤的判据会把后三张表整片判成「没有隔离」**。
// 这里用一张临时分区表把三种形态一次性钉住：
//
//	· 策略名任意（不是 project_isolation）；
//	· 多条策略（PERMISSIVE 之间是 OR，漏掉一条就是放宽隔离）；
//	· `FOR INSERT` 的策略只有 WITH CHECK、没有 USING（PostgreSQL 会拒绝不匹配的子句）。
func TestMigration507CopiesArbitraryPolicyShapes(t *testing.T) {
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
		// 任意策略名 + ALL 形态。
		"CREATE POLICY p_dbtest_a ON dbtest_part USING" +
			" (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)",
		// 同一张表上的第二条策略，只覆盖 INSERT（只有 WITH CHECK）。
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

	runSQLFile(t, db, reconcileSQLPath)

	enabled, forced, hasPolicy := rlsFlags(t, db, "dbtest_part_1")
	if !enabled || !forced || !hasPolicy {
		t.Fatalf("507 没有为临时分区补隔离：enable=%v force=%v policy=%v", enabled, forced, hasPolicy)
	}
	// 逐字比对谓词与形状（含 cmd / permissive / roles）。
	assertPredicateEqualsParent(t, db, "dbtest_part", "dbtest_part_1")

	// 两条策略都必须到位：少一条 PERMISSIVE 策略不是「少一点保护」，而是**放宽** ——
	// PERMISSIVE 策略之间是 OR 关系。
	var n int64
	if err := db.Raw("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid" +
		" WHERE c.relname = 'dbtest_part_1'").Scan(&n).Error; err != nil {
		t.Fatalf("统计分区策略失败: %v", err)
	}
	if n != 2 {
		t.Errorf("分区上的策略数 = %d，期望 2（父表两条都要复制）", n)
	}

	// 幂等：再跑一次不得报错、不得重复建策略。
	runSQLFile(t, db, reconcileSQLPath)
	var again int64
	if err := db.Raw("SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid" +
		" WHERE c.relname = 'dbtest_part_1'").Scan(&again).Error; err != nil {
		t.Fatalf("统计分区策略失败: %v", err)
	}
	if again != 2 {
		t.Errorf("重复执行 507 后策略数 = %d，期望仍为 2", again)
	}
	_ = ctx
}

// TestEnsureAheadRestoresDefaultPartitionRLS EnsureAhead 重建 DEFAULT 分区时必须补策略。
//
// 这是本批次发现的第二个缺口（不在指令书清单里）：EnsureAhead 建完月分区会调
// ensurePartitionRLS，但建 DEFAULT 分区那一段**没有**调 —— 而 DEFAULT 分区是数据兜底
// 落点（没有月分区可写的数据全部落进它）。稳态下它不重建，所以缺口平时看不出来；
// 一旦被手工 DROP / 误清理，重建出来的就是一个没有策略的桶。
//
// 红绿：把 EnsureAhead 里那一句 ensurePartitionRLS 调用去掉，本测试立刻变红。
func TestEnsureAheadRestoresDefaultPartitionRLS(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	const def = "page_views_default"
	// 迁移建出来的那份先确认是好的（否则后面的断言无意义）。
	if en, fo, pol := rlsFlags(t, db, def); !en || !fo || !pol {
		t.Fatalf("夹具不成立：迁移建出的 %s 隔离不完整（enable=%v force=%v policy=%v）", def, en, fo, pol)
	}

	if err := db.Exec("DROP TABLE " + def).Error; err != nil {
		t.Fatalf("删除 %s 失败: %v", def, err)
	}
	if kind := relkind(t, db, def); kind != "" {
		t.Fatalf("%s 应已被删除，实际 relkind=%q", def, kind)
	}

	if _, err := partition.EnsureAhead(ctx, db, 3); err != nil {
		t.Fatalf("EnsureAhead 失败: %v", err)
	}

	enabled, forced, hasPolicy := rlsFlags(t, db, def)
	if !enabled || !forced || !hasPolicy {
		t.Fatalf("EnsureAhead 重建的 %s 没有完整的工程隔离：enable=%v force=%v policy=%v ——"+
			"兜底桶裸奔比没有兜底桶更危险（前者静默、后者丢数据会报错）", def, enabled, forced, hasPolicy)
	}
	assertPredicateEqualsParent(t, db, "page_views", def)

	// 巡检也应当认它：重建后的分区不能成为新的缺口。
	gaps, _, err := partition.AuditPartitionRLSCoverage(ctx, db)
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("重建 DEFAULT 分区后巡检出现缺口：%v", gaps)
	}
}

// TestAuditRLSCoverageWhitelist 全库巡检：豁免名单由调用方传入，且巡检不是恒空。
//
// 双向断言是刻意的：只断言「传了豁免就为空」会被一条恒返回空的巡检骗过，
// 而恒空的门禁比没有门禁更糟（它给出已经检查过的错觉）。
//
// 豁免名单的**值**不在这里定义（那是工程判断，归口 docs/rules/database.md 与 pkg/rls）——
// 测试里写出来的这两个名字是审计 DB-01 的口径，用于对账，不是第二份真源。
func TestAuditRLSCoverageWhitelist(t *testing.T) {
	db := newPartitionFixture(t)
	ctx := context.Background()

	// ① 不带豁免：必须能看到那两张已知的「刻意不隔离」表 —— 证明巡检查得出东西。
	raw, checked, err := partition.AuditRLSCoverage(ctx, db)
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

	// ② 带豁免：除这两张外不应有别的缺口。将来若新增一张带 project_id 却没做 RLS 的表，
	// 这条会红 —— 那正是它应该有的行为（提醒补策略或补豁免理由）。
	gaps, _, err := partition.AuditRLSCoverage(ctx, db, "build_jobs", "product_outbox_events")
	if err != nil {
		t.Fatalf("巡检失败: %v", err)
	}
	if len(gaps) != 0 {
		t.Fatalf("豁免名单之外仍有 %d 处 RLS 缺口：%v", len(gaps), gaps)
	}
	t.Logf("受检 %d 张带 project_id 的基表，豁免 2 张后缺口 0 处", checked)
}

// TestPageSchedulesPageIndexSatisfiesOrderBy 506 的索引必须真的满足那条查询的「等值 + 排序」。
//
// 判据不是「索引存在」（那是 DDL 断言，看不出有没有用），而是 EXPLAIN 计划：
//   - 走 idx_page_schedules_page_created；
//   - 计划里**没有 Sort 节点** —— 这才证明排序由索引满足，而不是「取回整批再排」。
//     少了后半条，「索引加了但每次查询仍要排序」会被漏过：收益打折而没有任何信号。
//
// 数据分布是判据的一部分：3000 行集中在 5 个 page_id 上（模拟一个页面有多条历史排定），
// 且必须先 ANALYZE —— 空表上优化器必然选 Seq Scan，那种 EXPLAIN 不构成证据。
//
// 实测对照（真库，2026-10，3000 行 + ANALYZE）：
//
//	本查询                                          → Index Scan using idx_page_schedules_page_created，**无 Sort**
//	ListSchedulesForPages（page_id IN + status IN …）→ 仍走 idx_page_schedules_due + Sort
//
// 后者不是缺陷：终态行占绝大多数时，status 索引只扫回极少数行，比按 page_id 取回
// 每个页面的全部历史更便宜，优化器选得对。本索引的真正受益者是前者（面板片段，
// 全状态 + LIMIT 50）—— 460 那条 PARTIAL 索引恰恰覆盖不到终态行。
func TestPageSchedulesPageIndexSatisfiesOrderBy(t *testing.T) {
	db := newPartitionFixture(t)

	if err := db.Exec(`INSERT INTO page_schedules
		(page_id, lang, action, scheduled_at, status, draft_version, create_time, update_time)
		SELECT ('00000000-0000-0000-0000-00000000000' || (i % 5))::uuid, 'zh-CN', 'publish',
		       now() - (i || ' minutes')::interval, 'done', 1,
		       now() - (i || ' minutes')::interval, now()
		  FROM generate_series(1, 3000) AS i`).Error; err != nil {
		t.Fatalf("造测试数据失败: %v", err)
	}
	if err := db.Exec("ANALYZE page_schedules").Error; err != nil {
		t.Fatalf("ANALYZE 失败: %v", err)
	}

	var planJSON string
	if err := db.Raw("EXPLAIN (FORMAT JSON) SELECT * FROM page_schedules"+
		" WHERE page_id = ?::uuid ORDER BY create_time DESC, id DESC LIMIT 50",
		"00000000-0000-0000-0000-000000000001").Scan(&planJSON).Error; err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}

	nodes := explainNodeTypes(t, planJSON)
	t.Logf("计划节点：%v", nodes)

	if !strings.Contains(planJSON, "idx_page_schedules_page_created") {
		t.Errorf("计划没有用上 idx_page_schedules_page_created（506 的索引形同虚设）：\n%s", planJSON)
	}
	for _, n := range nodes {
		if n == "Sort" {
			t.Errorf("计划里出现 Sort 节点：索引没有满足 ORDER BY create_time DESC, id DESC\n%s", planJSON)
		}
	}
}

// explainNodeTypes 递归收集 EXPLAIN (FORMAT JSON) 计划里的所有 Node Type。
func explainNodeTypes(t *testing.T, planJSON string) []string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(planJSON), &decoded); err != nil {
		t.Fatalf("解析 EXPLAIN JSON 失败: %v\n原始输出：%s", err, planJSON)
	}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			if name, ok := node["Node Type"].(string); ok {
				out = append(out, name)
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(decoded)
	return out
}

// TestEnsureAheadReconcilesArbitraryPolicyShapes EnsureAhead 用的判据必须与迁移 507 同一份。
//
// 背景（本批次收口的差异）：改造前 EnsureAhead 走的是自己拼的 ensurePartitionRLS ——
// 它认死「策略名 project_isolation + ScopedPredicate」一种形态，而 507 是**通用复制**
// （逐条复制父表的 polname / polpermissive / polcmd / polroles / qual / withcheck）。
// 两份判据的差异不是学术问题：真库存在 4 个策略名，认死名字的那一份对别的形态
// 会补出一条**父表没有的**策略，同时**漏掉父表真实的那条**。
//
// 改造后的判据只有一份：EnsureAhead 末尾调 ReconcileRLS，而 ReconcileRLS 执行的
// 就是 migrations.ReconcilePartitionRLSSQL()（507 的 SQL 原文）。
//
// 红绿：把 ReconcileRLS 换回旧的「只补 project_isolation」逻辑，本测试立刻变红
// （临时分区上既没有父表的 p_dbtest_other，又凭空多出一条 project_isolation）。
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
		// 分区在**建表时**就有了，但没有任何策略 —— 模拟「部署月落在 215 名单之外」的历史分区。
		"CREATE TABLE dbtest_ahead_1 PARTITION OF dbtest_ahead" +
			" FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')",
		// 策略名刻意不用 project_isolation（真库的 p_comments_project 等就是这个形态）。
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

	// EnsureAhead 只处理 Tables 里的三张表，但它末尾要跑全库对账 —— 本用例验证的正是那一句。
	if _, err := partition.EnsureAhead(ctx, db, 3); err != nil {
		t.Fatalf("EnsureAhead 失败: %v", err)
	}

	enabled, forced, hasPolicy := rlsFlags(t, db, "dbtest_ahead_1")
	if !enabled || !forced || !hasPolicy {
		t.Fatalf("EnsureAhead 之后临时分区仍无隔离：enable=%v force=%v policy=%v"+
			"（EnsureAhead 与迁移 507 的判据不同源）", enabled, forced, hasPolicy)
	}
	assertPredicateEqualsParent(t, db, "dbtest_ahead", "dbtest_ahead_1")

	// 反向断言：不许凭空多出父表没有的策略。多出的那条如果是 PERMISSIVE，
	// 就与父表的策略形成 OR —— 那不是「多一点保护」，是放宽。
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

// TestTableTimeColumnsMatchPartitionKeys Table.TimeColumn 的语义是「分区键（时间列）」，
// 所以判据不是「这个列存在」，而是「它就是库里真实的分区键」。
//
// 为什么要这样钉：该字段**当前不被任何生产代码消费**（EnsureAhead 只用 Name，
// 分区键在 CREATE TABLE … PARTITION OF 时由父表继承）。一个不被消费、又没人核对的字段，
// 迟早会写错 —— 而它一旦写错，第一个拿它拼「按时间列删/归档」SQL 的人会拿到运行期错误
// 或者更糟：删错范围。本用例把它与库里的真值（pg_get_partkeydef）绑在一起。
//
// 红绿：把任一 TimeColumn 改成错值（例如 page_views 写成 created_at），本用例立刻变红。
func TestTableTimeColumnsMatchPartitionKeys(t *testing.T) {
	db := newPartitionFixture(t)

	for _, tb := range partition.Tables {
		var partkey string
		if err := db.Raw("SELECT pg_get_partkeydef(c.oid) FROM pg_class c"+
			" JOIN pg_namespace n ON n.oid = c.relnamespace"+
			" WHERE n.nspname = current_schema() AND c.relname = ?", tb.Name).Scan(&partkey).Error; err != nil {
			t.Fatalf("读取 %s 的分区键失败: %v", tb.Name, err)
		}
		if partkey == "" {
			t.Fatalf("%s 不是分区表（夹具不成立）", tb.Name)
		}
		// pg_get_partkeydef 形如 "RANGE (create_time)"；只断列名在里面，
		// 不断整串 —— 未来换分区策略（LIST / HASH）不该让这条用例变红。
		if !strings.Contains(partkey, "("+tb.TimeColumn+")") {
			t.Errorf("%s.TimeColumn = %q，但库里真实的分区键是 %q ——"+
				"声明的分区键与真值不符（该字段无消费方，不会在别处报错）",
				tb.Name, tb.TimeColumn, partkey)
		}
		t.Logf("%s 分区键 = %s（声明 %s）", tb.Name, partkey, tb.TimeColumn)
	}
}
