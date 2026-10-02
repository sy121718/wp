// Package partition 时间序列表的分区维护（审计 DB-004）。
//
// 迁移 173 把三张只增表（page_views / inventory_stock_movements / master_data_changes）
// 改成了按月 RANGE 分区。分区不会自己维护：没有分区可写的数据会落到 DEFAULT 分区
// （迁移里刻意建了它，避免访客打点那种静默写入路径直接丢数据），但那等于分区白做 ——
// 收益全在「数据按时间分桶」，桶不建就没有桶。
//
// 因此本包负责两件日常：
//   - EnsureAhead：提前建出未来若干个月的分区（启动时 + 每日各一次）；
//   - DetachBefore：把早于某个月的分区**摘下来**（DETACH 不是 DROP，数据仍在，可归档）。
//
// 刻意不用 pg_partman：那是一个需要额外安装、有自己元数据表的扩展，
// 而这里要的就是「按月建表 + 按月摘表」两件小事，十几行 SQL 能表达清楚。
//
// 与迁移包的关系（DB-02）：分区隔离对账（ReconcileRLS）**执行的是迁移 507 的 SQL 原文**，
// 见 migrations.ReconcilePartitionRLSSQL —— 判据只有一份，改那边这里同时变。
package partition

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	migrations "go_wp/public/migrations"

	"go_wp/pkg/logger"
)

// Table 一张分区表的声明。
type Table struct {
	// Name 分区父表名。
	Name string
	// TimeColumn 分区键（时间列）。
	TimeColumn string
}

// Tables 需要维护的三张表（与迁移 173 一致）。
var Tables = []Table{
	{Name: "page_views", TimeColumn: "viewed_at"},
	{Name: "inventory_stock_movements", TimeColumn: "create_time"},
	{Name: "master_data_changes", TimeColumn: "create_time"},
}

// AheadMonths 默认提前建分区的时间跨度。
//
// 取 3 个月：维护任务每日跑，一次故障三天内被发现也还留着两个月余量；
// 太短（1 个月）会让「任务停了一个月」直接演变成缺分区，太长只是多几张空表。
const AheadMonths = 3

// monthRange 返回某个月的起止（[from, to) 半开区间）。
func monthRange(month time.Time) (from, to time.Time) {
	u := month.UTC()
	from = time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0)
}

// partitionName 月分区名（表名 + YYYY_MM）。
func partitionName(table string, month time.Time) string {
	return fmt.Sprintf("%s_%s", table, month.UTC().Format("2006_01"))
}

// EnsureAhead 确保每张分区表从「上个月」到「未来 aheadMonths 个月」的分区都存在，
// 返回本次执行过的建分区语句对应的分区名。
//
// 幂等：CREATE TABLE IF NOT EXISTS，重复执行只是把已存在的跳过。
// 单张表失败不影响其它表（分区缺失是「写不进去」级别的问题，越早暴露越好，
// 但不该因为一张表的问题让另外两张也不建）。
func EnsureAhead(ctx context.Context, db *gorm.DB, aheadMonths int) (created []string, err error) {
	if db == nil {
		return nil, nil
	}
	if aheadMonths <= 0 {
		aheadMonths = AheadMonths
	}
	thisMonth, _ := monthRange(time.Now())
	for _, t := range Tables {
		for i := -1; i <= aheadMonths; i++ {
			month := thisMonth.AddDate(0, i, 0)
			name := partitionName(t.Name, month)
			from, to := monthRange(month)
			stmt := fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
				name, t.Name, from.Format("2006-01-02"), to.Format("2006-01-02"))
			if cerr := db.WithContext(ctx).Exec(stmt).Error; cerr != nil {
				logger.Scene("partition").With("table", t.Name).With("partition", name).
					Error(cerr, "创建月分区失败")
				continue
			}
			created = append(created, name)
		}
		// DEFAULT 分区兜底（迁移里已建，这里再保一次：被手工删掉时下次任务补回来）。
		def := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s_default PARTITION OF %s DEFAULT", t.Name, t.Name)
		if derr := db.WithContext(ctx).Exec(def).Error; derr != nil {
			logger.Scene("partition").With("table", t.Name).Error(derr, "创建 DEFAULT 分区失败")
		}
	}

	// 全部表的分区都建完之后，统一跑一次工程隔离对账（DB-02）。
	//
	// 为什么是「统一一次」而不是「建一张补一张」：判据只有一份 ——
	// migrations.ReconcilePartitionRLSSQL()（迁移 507 的原文）。本函数**不再自己拼策略**：
	// 那样会与本包的另一处、以及 507 本身形成第二份实现，而两份等价的实现必然漂移，
	// 漂移在这里的后果是静默的 —— 某个分区少一条策略，应用走父表一切正常，
	// 只有按分区名直查才绕开隔离。
	//
	// 代价是一轮只读检查（稳态下 507 走快速路径、零 DDL），换来的是「改一处两边同步变」。
	// 失败只记日志：分区建好了、对账没跑成不是「分区缺失」级别的问题，
	// 而 migrations 执行与每日任务都会再对一次。
	if rerr := ReconcileRLS(ctx, db); rerr != nil {
		logger.Scene("partition").Error(rerr, "分区工程隔离对账失败（分区已建，策略可能未补齐）")
	}
	return created, nil
}

// ReconcileRLS 对账全库分区的工程隔离：把每张分区表父表的策略逐条复制到它的叶子分区。
//
// 实现**就是迁移 507 的 SQL 原文**（migrations.ReconcilePartitionRLSSQL），不是等价实现、
// 不是第二份逻辑 —— 改那份 SQL，迁移与这里同时变。幂等：稳态只做只读检查、零 DDL。
//
// 为什么它可以被反复调用：分区是持续被创建的对象（EnsureAhead 按月建 + DEFAULT 兜底），
// 而 PG 的 ENABLE / FORCE ROW LEVEL SECURITY **不递归到分区**、policy 也不继承
// （实测对父表 ENABLE/FORCE 之后，父表 relrowsecurity = t 而所有子表仍为 f）。
func ReconcileRLS(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := db.WithContext(ctx).Exec(migrations.ReconcilePartitionRLSSQL()).Error; err != nil {
		return fmt.Errorf("执行分区隔离对账失败: %w", err)
	}
	return nil
}

// Partition 一个月分区。
type Partition struct {
	// Name 分区表名。
	Name string
	// Month 该分区覆盖的月份（由分区名解析）。
	Month time.Time
}

// ListPartitions 列出一张表的月分区（不含 DEFAULT 分区），按月升序。
func ListPartitions(ctx context.Context, db *gorm.DB, table string) (list []Partition, err error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.WithContext(ctx).Raw(
		"SELECT c.relname AS name"+
			" FROM pg_class c"+
			" JOIN pg_inherits i ON i.inhrelid = c.oid"+
			" JOIN pg_class p ON p.oid = i.inhparent"+
			" JOIN pg_namespace n ON n.oid = p.relnamespace"+
			" WHERE n.nspname = current_schema() AND p.relname = ? AND c.relname <> p.relname || '_default'",
		table).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	prefix := table + "_"
	for rows.Next() {
		var name string
		if serr := rows.Scan(&name); serr != nil {
			return list, serr
		}
		month, ok := parsePartitionMonth(name, prefix)
		if !ok {
			continue
		}
		list = append(list, Partition{Name: name, Month: month})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// 按月升序（插入排序：分区数量是「月数」量级，不值得再引一个排序依赖）。
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Month.Before(list[j-1].Month); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list, nil
}

// parsePartitionMonth 从分区名解析月份（page_views_2026_09 → 2026-09 的月初）。
func parsePartitionMonth(name, prefix string) (time.Time, bool) {
	if len(name) != len(prefix)+7 || name[:len(prefix)] != prefix {
		return time.Time{}, false
	}
	month, err := time.ParseInLocation("2006_01", name[len(prefix):], time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return month, true
}

// DetachBefore 把早于 cutoff 所在月的分区 DETACH 出来（**不删除**：数据留在摘下来的表里，供归档）。
//
// DETACH 而不是 DROP：DETACH 之后数据仍是普通表，可以 dump / 搬运 / 事后删除，
// 而 DROP 是不可逆的。运维的归档动作因此分两步：摘下（本函数）+ 确认后处理。
func DetachBefore(ctx context.Context, db *gorm.DB, table string, cutoff time.Time) (detached []string, err error) {
	if db == nil {
		return nil, nil
	}
	cutoffMonth, _ := monthRange(cutoff)
	parts, err := ListPartitions(ctx, db, table)
	if err != nil {
		return nil, err
	}
	for _, p := range parts {
		if !p.Month.Before(cutoffMonth) {
			continue
		}
		if derr := db.WithContext(ctx).
			Exec(fmt.Sprintf("ALTER TABLE %s DETACH PARTITION %s", table, p.Name)).Error; derr != nil {
			logger.Scene("partition").With("table", table).With("partition", p.Name).
				Error(derr, "摘除月分区失败")
			continue
		}
		detached = append(detached, p.Name)
	}
	return detached, nil
}

// maintenanceInterval 分区维护间隔。
const maintenanceInterval = 24 * time.Hour

// StartScheduler 启动分区维护（启动时一次 + 每日一次）。
//
// 与其它调度同形：先跑一次再等间隔。多实例同时跑是安全的 ——
// 建分区走 CREATE TABLE IF NOT EXISTS，重复执行不产生副作用。
func StartScheduler(ctx context.Context, db *gorm.DB) {
	if db == nil {
		return
	}
	go func() {
		run := func() {
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			created, err := EnsureAhead(runCtx, db, AheadMonths)
			if err != nil {
				logger.Scene("partition").Error(err, "分区维护失败")
				return
			}
			if len(created) > 0 {
				logger.Scene("partition").With("count", len(created)).Info("已补齐未来分区")
			}
			// 对账（审计 DB-02）：分区刚建/补过，这里确认「隔离也跟着到位了」。
			//
			// 只报不修：修复路径是迁移 507（每次启动执行一次，覆盖存量）与 EnsureAhead
			// 自己（覆盖刚建的）。巡检一旦开始改库，「谁动了这个库」就失去单一来源，
			// 而且真正需要人工介入的场景（有人手工 DROP 了策略）恰恰应该留痕给人看。
			//
			// 用零外部输入的 AuditPartitionRLSCoverage：它问的是「根表有策略则叶子必须
			// 有」，不需要豁免名单 —— 日常任务里传一份会漂移的白名单，换来的只是假警报。
			if gaps, checked, aerr := AuditPartitionRLSCoverage(runCtx, db); aerr != nil {
				logger.Scene("partition").Error(aerr, "分区工程隔离对账失败")
			} else if len(gaps) > 0 {
				logger.Scene("partition").With("checked", checked).With("gaps", len(gaps)).
					Warn("发现分区工程隔离缺口：按分区名直查会绕过工程隔离（应用走父表不受影响）")
				for _, g := range gaps {
					logger.Scene("partition").
						With("table", g.Table).With("parent", g.Parent).With("missing", g.Missing).
						Warn("分区缺 ENABLE / FORCE / 策略")
				}
			}
		}
		run()
		ticker := time.NewTicker(maintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// ===========================================================================
// 工程隔离覆盖巡检（审计 DB-02）
// ===========================================================================
//
// 分区的隔离是**两处共建**的：迁移 507 负责对账存量（每次启动执行一次），
// EnsureAhead 负责新建（建完当月分区立刻补策略）。两处都在「写」，
// 于是需要一个只读的判据回答「现在还有没有缺口」—— 否则「修好了」这件事
// 只能靠读代码相信，而这一类缺口的特征恰恰是**没有任何报错路径**：
// 应用走父表，功能完全正常，只有按分区名直查时才绕开隔离。
//
// 这里有两个判据，分工不要混：
//   - AuditPartitionRLSCoverage：**分区专属**，零外部输入（判据是「根表有策略则叶子必须有」），
//     可以被日常任务安全调用；
//   - AuditRLSCoverage：**全库门禁**，要求调用方传豁免名单 —— 因为「哪些带 project_id 的
//     表可以不做隔离」是工程判断，名单的归口不在本包（见该函数注释）。

// RLSGap 一处工程隔离缺口。
type RLSGap struct {
	// Table 缺口的表名（分区子表或基表）。
	Table string
	// Parent 分区表的根表名（仅分区缺口非空）。
	Parent string
	// Missing 缺了什么，逗号分隔：enable / force / policy。
	Missing string
}

// rlsCoverageRow 巡检查询的原始行（三个巡检函数共用形状）。
type rlsCoverageRow struct {
	Table     string `gorm:"column:table_name"`
	Parent    string `gorm:"column:parent_name"`
	Enabled   bool   `gorm:"column:enabled"`
	Forced    bool   `gorm:"column:forced"`
	HasPolicy bool   `gorm:"column:has_policy"`
}

// gapOf 由一行巡检结果组装缺口；三项齐备时返回 ok=false（这不是缺口）。
func (r rlsCoverageRow) gapOf() (RLSGap, bool) {
	var missing []string
	if !r.Enabled {
		missing = append(missing, "enable")
	}
	if !r.Forced {
		missing = append(missing, "force")
	}
	if !r.HasPolicy {
		missing = append(missing, "policy")
	}
	if len(missing) == 0 {
		return RLSGap{}, false
	}
	return RLSGap{Table: r.Table, Parent: r.Parent, Missing: strings.Join(missing, ",")}, true
}

// partitionRLSCoverageSQL 分区子表的隔离覆盖查询。
//
// 判据：「根表上有策略」的分区表，它的每个叶子分区都要有 ENABLE + FORCE，且策略名集合
// 覆盖父表 —— 与迁移 507 逐条对应，两份判据必须同源，否则会出现「巡检说绿、迁移没修」
// 或反过来这种自相矛盾的状态。
//
// **不按策略名过滤**（这里曾经写成 `polname = 'project_isolation'`，是错的）：
// 全库实际存在 4 个策略名 —— `project_isolation`（迁移 215 铺开的 40+ 张表）
// 与 `p_comments_project` / `p_membership_tiers_project` / `p_membership_assignments_project`
// （462/466 系列自建）。按名字过滤会把后三张表整片判成「没有隔离」——误报，
// 而误报会让人开始忽略这个巡检。
//
// 用 pg_partition_tree 而不是只 join 一层 pg_inherits：嵌套分区下叶子分区的直接
// 父级是中间层，只看一层会按中间层判定（中间层没有策略就整片跳过，缺口静默保留）。
const partitionRLSCoverageSQL = `
SELECT leaf.relname                AS table_name,
       root.relname                AS parent_name,
       leaf.relrowsecurity         AS enabled,
       leaf.relforcerowsecurity    AS forced,
       NOT EXISTS (
           SELECT pol.polname FROM pg_policy pol WHERE pol.polrelid = root.oid
           EXCEPT
           SELECT pol2.polname FROM pg_policy pol2 WHERE pol2.polrelid = leaf.oid
       )                           AS has_policy
  FROM pg_class root
  JOIN pg_namespace n ON n.oid = root.relnamespace
  CROSS JOIN LATERAL pg_partition_tree(root.oid) AS pt
  JOIN pg_class leaf ON leaf.oid = pt.relid
 WHERE n.nspname = current_schema()
   AND root.relkind = 'p'
   AND pt.isleaf
   AND EXISTS (SELECT 1 FROM pg_policy pol WHERE pol.polrelid = root.oid)
 ORDER BY 1`

// AuditPartitionRLSCoverage 巡检分区子表的工程隔离覆盖，返回缺口清单与受检分区数。
//
// 为什么这个判据不需要白名单：它不问「这张表该不该有隔离」（工程判断），只问
// 「根表已经有了，它的分区跟上了没有」。刻意不隔离的表（如 build_jobs）根本没有
// 根表策略，自然不进受检集合 —— 于是本函数可以被日常任务直接调用而不产生假警报。
func AuditPartitionRLSCoverage(ctx context.Context, db *gorm.DB) (gaps []RLSGap, checked int, err error) {
	if db == nil {
		return nil, 0, nil
	}
	var rows []rlsCoverageRow
	if err = db.WithContext(ctx).Raw(partitionRLSCoverageSQL).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		checked++
		if gap, ok := r.gapOf(); ok {
			gaps = append(gaps, gap)
		}
	}
	return gaps, checked, nil
}

// fullRLSCoverageSQL 全库基表（relkind='r'）中带 project_id 的表的隔离覆盖查询。
//
// 只查真实存储表：分区父表的 relkind 是 'p'，它的隔离由迁移 215 负责，
// 而它的每个分区子表都要单独检查（那正是 DB-02 的缺口形态，由上面的
// AuditPartitionRLSCoverage 覆盖，本函数也会把叶子分区一并列出）。
//
// has_policy 的判据是「这张表上有没有**任意**策略」，不按策略名过滤 —— 理由见
// partitionRLSCoverageSQL 的注释（全库有 4 个策略名）。
//
// **本判据回答「有没有覆盖」，不回答「覆盖得对不对」**：一条谓词写成 `true` 的策略
// 也会被算作已覆盖。谓词内容的对账归 215 与 pkg/rls 的常量（那份判据在别处，
// 这里复制一份就是第二份真源）。留着这个边界的代价是「假绿」——比误报危险，
// 所以写在这里而不是藏着。
const fullRLSCoverageSQL = `
SELECT c.relname             AS table_name,
       ''                    AS parent_name,
       c.relrowsecurity      AS enabled,
       c.relforcerowsecurity AS forced,
       EXISTS (SELECT 1 FROM pg_policy pol WHERE pol.polrelid = c.oid) AS has_policy
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname = current_schema()
   AND c.relkind = 'r'
   AND EXISTS (SELECT 1 FROM pg_attribute a
                WHERE a.attrelid = c.oid
                  AND a.attname = 'project_id'
                  AND a.attnum > 0
                  AND NOT a.attisdropped)
 ORDER BY 1`

// AuditRLSCoverage 巡检「所有带 project_id 的基表是否都有完整工程隔离」。
//
// 豁免名单由**调用方**传入（如 build_jobs / product_outbox_events）：
// 「这张表为什么可以不带隔离」是工程判断，归口在 docs/rules/database.md 与 pkg/rls
// 的运行时探针，本包不复制那份名单 —— 复制出来的第二份真源必然会与被复制的那份漂移，
// 而漂移的表现是「巡检放过了一张本该被拦的表」或反之。
//
// 三类判断的边界（都写在同一处，避免被读成同一件事）：
//   - 本函数：「带 project_id 的基表有没有覆盖」—— 需要调用方给豁免名单；
//   - AuditPartitionRLSCoverage：「根表有策略则叶子分区必须有」—— **零外部输入**，
//     所以能进日常任务而不产生假警报；
//   - 「没有实现也没有 project_id」的保留表（如 page_component_pins）：与隔离无关，
//     记录在 docs/schema-snapshot.md §3.1。
//
// 返回的 gaps **不含**豁免表；checked 是受检表总数（含豁免）。
func AuditRLSCoverage(ctx context.Context, db *gorm.DB, exempt ...string) (gaps []RLSGap, checked int, err error) {
	if db == nil {
		return nil, 0, nil
	}
	skip := make(map[string]struct{}, len(exempt))
	for _, name := range exempt {
		skip[strings.TrimSpace(name)] = struct{}{}
	}
	var rows []rlsCoverageRow
	if err = db.WithContext(ctx).Raw(fullRLSCoverageSQL).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		checked++
		if _, ok := skip[r.Table]; ok {
			continue
		}
		if gap, ok := r.gapOf(); ok {
			gaps = append(gaps, gap)
		}
	}
	return gaps, checked, nil
}
