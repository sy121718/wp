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
package partition

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

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
	return created, nil
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
