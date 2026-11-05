package partition

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/logger"
)

type Table struct {
	Name string

	TimeColumn string
}

// Options 是应用注入的表目录与隔离对账 SQL。
type Options struct {
	Tables []Table

	ReconcileRLSSQL func() string
}

const AheadMonths = 3

func monthRange(month time.Time) (from, to time.Time) {
	u := month.UTC()
	from = time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0)
}

func partitionName(table string, month time.Time) string {
	return fmt.Sprintf("%s_%s", table, month.UTC().Format("2006_01"))
}

func EnsureAhead(ctx context.Context, db *gorm.DB, opts Options, aheadMonths int) (created []string, err error) {
	if db == nil {
		return nil, nil
	}
	if aheadMonths <= 0 {
		aheadMonths = AheadMonths
	}
	thisMonth, _ := monthRange(time.Now())
	for _, t := range opts.Tables {
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

		def := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s_default PARTITION OF %s DEFAULT", t.Name, t.Name)
		if derr := db.WithContext(ctx).Exec(def).Error; derr != nil {
			logger.Scene("partition").With("table", t.Name).Error(derr, "创建 DEFAULT 分区失败")
		}
	}

	if rerr := ReconcileRLS(ctx, db, opts.ReconcileRLSSQL); rerr != nil {
		logger.Scene("partition").Error(rerr, "分区工程隔离对账失败（分区已建，策略可能未补齐）")
	}
	return created, nil
}

func ReconcileRLS(ctx context.Context, db *gorm.DB, sql func() string) error {
	if db == nil || sql == nil {
		return nil
	}
	if err := db.WithContext(ctx).Exec(sql()).Error; err != nil {
		return fmt.Errorf("执行分区隔离对账失败: %w", err)
	}
	return nil
}

type Partition struct {
	Name string

	Month time.Time
}

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

	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Month.Before(list[j-1].Month); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	return list, nil
}

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

const maintenanceInterval = 24 * time.Hour

func StartScheduler(ctx context.Context, db *gorm.DB, opts Options) {
	if db == nil {
		return
	}
	go func() {
		run := func() {
			runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			created, err := EnsureAhead(runCtx, db, opts, AheadMonths)
			if err != nil {
				logger.Scene("partition").Error(err, "分区维护失败")
				return
			}
			if len(created) > 0 {
				logger.Scene("partition").With("count", len(created)).Info("已补齐未来分区")
			}

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

type RLSGap struct {
	Table string

	Parent string

	Missing string
}

type rlsCoverageRow struct {
	Table     string `gorm:"column:table_name"`
	Parent    string `gorm:"column:parent_name"`
	Enabled   bool   `gorm:"column:enabled"`
	Forced    bool   `gorm:"column:forced"`
	HasPolicy bool   `gorm:"column:has_policy"`
}

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
