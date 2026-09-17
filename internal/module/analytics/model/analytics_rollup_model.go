package analyticsmodel

// analytics_rollup_model.go — 访问统计的按天预聚合（审计 DB-005 / IDX-010）。
//
// page_views 只增不减，而统计查询几乎总是查「过去 N 天」—— 这段数据一旦落定就不会再变。
// 明细表因此只承担两件事：接收当天的实时写入、支持下钻；历史窗口一律读预聚合。
//
// 汇总的正确性靠「全量重算 + 幂等覆盖」而不是增量累加：某一天被重算多少次，
// 结果都等于从明细重新算一遍的值（ON CONFLICT DO UPDATE）。增量累加一旦漏算或重算，
// 偏差会永久留在汇总里且无法自查 —— 重算没有这个问题。

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// tableNameDailyStats 日汇总表。
//
// 复用迁移 161 建的 page_views_daily（当时注释写明「先加日汇总表……供清理任务使用」，
// 但一直零消费），由迁移 170 扩到 (project_id, day, scope, path) 四列主键。
// 有意不另起一张表：同一份语义建两张，早晚会有人往错的那张写。
const tableNameDailyStats = "page_views_daily"

// 汇总粒度（与 DDL 的 CHECK 逐字对应）。
const (
	// ScopeAll 工程级当日总计（path 固定空串）。
	ScopeAll = "all"
	// ScopePath 路径级当日聚合（路径排行用）。
	ScopePath = "path"
)

// DailyStatEntity 预聚合表实体。
type DailyStatEntity struct {
	ProjectID string    `gorm:"column:project_id;primaryKey"`
	Day       time.Time `gorm:"column:day;primaryKey"`
	Scope     string    `gorm:"column:scope;primaryKey"`
	Path      string    `gorm:"column:path;primaryKey"`
	Views     int64     `gorm:"column:views;not null"`
	Visitors  int64     `gorm:"column:visitors;not null"`
	RolledAt  time.Time `gorm:"column:rolled_at;not null"`
}

// TableName 表名。
func (DailyStatEntity) TableName() string { return tableNameDailyStats }

// 聚合 SQL：与明细口径逐字一致（COUNT(*) 与 COUNT(DISTINCT NULLIF(visitor_hash,”))）。
// 一致性是这套方案的前提 —— 汇总与明细算的是两套公式的话，
// 「切到汇总之后数字变了」就会变成一个没人能解释的现象。
const (
	// all 行用 VALUES + 标量子查询而不是 INSERT ... SELECT：后者在「当天 0 访问」时
	// 一行都不写，于是「这一天汇总过没有」无从判断，查询侧就只能保守地退回明细
	// （性能白优化）。写一行 views=0 让「已汇总」成为一个可判定的状态。
	rollupAllSQL = `INSERT INTO page_views_daily (project_id, day, scope, path, views, visitors, rolled_at)
		VALUES (?,
		        ?::date,
		        'all',
		        '',
		        (SELECT COUNT(*) FROM page_views WHERE project_id = ? AND viewed_at >= ? AND viewed_at < ?),
		        (SELECT COUNT(DISTINCT NULLIF(visitor_hash, '')) FROM page_views WHERE project_id = ? AND viewed_at >= ? AND viewed_at < ?),
		        now())
		ON CONFLICT (project_id, day, scope, path)
		DO UPDATE SET views = EXCLUDED.views, visitors = EXCLUDED.visitors, rolled_at = now()`

	rollupPathSQL = `INSERT INTO page_views_daily (project_id, day, scope, path, views, visitors, rolled_at)
		SELECT ?, ?::date, 'path', path, COUNT(*), COUNT(DISTINCT NULLIF(visitor_hash, '')), now()
		FROM page_views
		WHERE project_id = ? AND viewed_at >= ? AND viewed_at < ?
		GROUP BY path
		ON CONFLICT (project_id, day, scope, path)
		DO UPDATE SET views = EXCLUDED.views, visitors = EXCLUDED.visitors, rolled_at = now()`

	// 清理重算后已不存在的路径行：明细被清理（保留期到期）后，汇总里对应的路径行
	// 若不删就会永远留着，路径排行里出现一个点不开、也查不到明细的幽灵条目。
	rollupCleanupSQL = `DELETE FROM page_views_daily s
		WHERE s.project_id = ? AND s.day = ?::date AND s.scope = 'path'
		  AND NOT EXISTS (
		    SELECT 1 FROM page_views v
		    WHERE v.project_id = s.project_id AND v.path = s.path
		      AND v.viewed_at >= ? AND v.viewed_at < ?)
	`
)

// RollupDay 重算某一天（UTC 日界）的汇总，from/to 是该日的半开区间。
//
// 幂等：同一天跑多少次，结果都等于明细的当前状态。
func (m *Model) RollupDay(ctx context.Context, projectID string, day, from, to time.Time) (err error) {
	// 三条语句读写的是 page_views_daily 与 page_views（两张都带策略），必须在同一
	// 工程作用域里跑：汇总写入若被策略挡下，水位（LastRolledDay）却推进了 —— 会留下
	// 一整段「已汇总但数字为 0」的历史，且没有任何报错。
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err := tx.Exec(rollupAllSQL, projectID, day, projectID, from, to, projectID, from, to).Error; err != nil {
			return err
		}
		if err := tx.Exec(rollupPathSQL, projectID, day, projectID, from, to).Error; err != nil {
			return err
		}
		// 参数与占位符逐个对齐：cleanup 里的 project_id 只出现一次（子查询用 s.project_id 关联），
		// 多传一个会得到「mismatched param and argument count」——那种错误不会指认是哪条语句。
		return tx.Exec(rollupCleanupSQL, projectID, day, from, to).Error
	})
}

// ListProjectsWithViews 列出有访问明细的工程（汇总任务的输入）。
//
// 取「有访问的工程」而不是 projects 全表：没有访问的工程不需要汇总行，
// 汇总任务也不该被空工程拖着跑。
//
// 实现形状（DB-009 第七批）：工程清单取自 projects 表 —— 它是隔离的**主体**
// （没有 project_id 列、不在迁移 215 的 53 个对象里），读它不涉及任何被隔离数据 ——
// 再**逐工程在作用域内**探测明细是否存在。原来的写法是
// SELECT DISTINCT project_id FROM page_views，而 page_views 带 FORCE 策略、
// 谓词读会话变量 app.project_id：没有作用域时那条 SELECT 恒返回空集（fail closed 不报错）。
// 失效形态是最难发现的一种 —— 汇总任务照常每小时跑，RollupRecent 返回 projects=0，
// 日志里一行异常都没有，站点只是「历史窗口的数字一直比明细少」。
func (m *Model) ListProjectsWithViews(ctx context.Context) (ids []string, err error) {
	candidates, err := m.listAllProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	ids = make([]string, 0, len(candidates))
	for _, pid := range candidates {
		if cerr := ctx.Err(); cerr != nil {
			return ids, cerr
		}
		var n int64
		// 作用域逐个建：会话变量是单值，多个工程不可能并进一次查询。
		if err = rls.InProjectScope(ctx, m.db, pid, func(tx *gorm.DB) error {
			return tx.Model(&PageViewEntity{}).Where("project_id = ?", pid).Limit(1).Count(&n).Error
		}); err != nil {
			return nil, err
		}
		if n > 0 {
			ids = append(ids, pid)
		}
	}
	return ids, nil
}

// listAllProjectIDs 全部站点工程 id（逐工程扇出的清单来源）。
//
// 读 projects 表不需要工程作用域：那是隔离的主体而不是被隔离的数据。
// analytics 侧已有先例（ListRetentionPolicies 同样直读 projects），落点与口径一致。
func (m *Model) listAllProjectIDs(ctx context.Context) (ids []string, err error) {
	err = m.db.WithContext(ctx).
		Raw("SELECT id::text FROM projects ORDER BY create_time ASC, id ASC").Scan(&ids).Error
	return ids, err
}

// rollupScope 汇总表的查询入口（本 model 内部使用：汇总表与明细表同属访问统计）。
func (m *Model) rollupScope(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Table(tableNameDailyStats)
}

// utcDayOf 取某时刻所在 UTC 日的零点。
//
// 与 service 的 dayStart 同口径（UTC 日界）：两处必须一致，否则「补齐到哪里」
// 与「查询窗口含几天」会各算各的，补齐永远追不上查询的判定。
func utcDayOf(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// EarliestViewDay 该工程最早一条明细所在的 UTC 日（无明细时 ok=false）。
//
// 用途是「首次汇总从哪一天开始」：不从头补起的话，历史窗口读汇总会读到空，
// 数字比明细少 —— 那比查得慢严重得多。
func (m *Model) EarliestViewDay(ctx context.Context, projectID string) (day time.Time, ok bool, err error) {
	// 用 sql.NullTime 接聚合结果：MIN(viewed_at) 在无行时是 NULL，
	// 而 GORM 无法把 NULL 扫进 *time.Time（报 unsupported destination）——
	// 那会变成「读取水位失败」，进而让汇总整段跳过。
	var raw sql.NullTime
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Where("project_id = ?", projectID).
			Select("MIN(viewed_at) AS min_viewed_at").Scan(&raw).Error
	})
	if err != nil || !raw.Valid {
		return time.Time{}, false, err
	}
	return utcDayOf(raw.Time), true, nil
}

// LastRolledDay 该工程「已汇总」的最后一天（无汇总行时 ok=false）。
//
// 只看 scope='all' 行：path 行的存在取决于当天有没有访问，不能用来判断水位。
func (m *Model) LastRolledDay(ctx context.Context, projectID string) (day time.Time, ok bool, err error) {
	var raw sql.NullTime
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("MAX(day) AS max_day").
			Where("project_id = ? AND scope = ?", projectID, ScopeAll).
			Scan(&raw).Error
	})
	if err != nil || !raw.Valid {
		return time.Time{}, false, err
	}
	return utcDayOf(raw.Time), true, nil
}

// CountRolledDays 统计窗口内「已汇总」的天数（供查询侧判断能否走汇总表）。
func (m *Model) CountRolledDays(ctx context.Context, projectID string, from, to time.Time) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
			Count(&n).Error
	})
	return n, err
}

// RollupTotals 历史窗口的合计（PV / UV），只读汇总表。
func (m *Model) RollupTotals(ctx context.Context, projectID string, from, to time.Time) (views, visitors int64, err error) {
	var row struct {
		Views    int64 `gorm:"column:views"`
		Visitors int64 `gorm:"column:visitors"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("COALESCE(SUM(views), 0) AS views, COALESCE(SUM(visitors), 0) AS visitors").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
			Scan(&row).Error
	})
	if err != nil {
		return 0, 0, err
	}
	return row.Views, row.Visitors, nil
}

// RollupByDay 历史窗口的按天序列（升序；只含有点击的天，与明细口径一致）。
//
// HAVING SUM(views) > 0 是必须的：汇总表为「已汇总但当天没有访问」的日子也写行
// （views=0，见 rollupAllSQL 的注释 —— 那一行是「已汇总」的凭据），
// 若不在查询侧滤掉，报表里会冒出一串明细口径下不存在的零值天。
func (m *Model) RollupByDay(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("day, SUM(views) AS views, SUM(visitors) AS visitors").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
			Group("day").Having("SUM(views) > 0").Order("day ASC").Scan(&rows).Error
	})
	return rows, err
}

// RollupPathTotal 历史窗口内出现过的不同路径数（路径排行分页用）。
func (m *Model) RollupPathTotal(ctx context.Context, projectID string, from, to time.Time) (total int64, err error) {
	// COUNT(DISTINCT path) 而不是 COUNT(*) + GROUP BY path：
	// 后者按分组返回多行，而这里 Scan 到单个标量只会拿到第一行 ——
	// 得到的数字看着像「路径数」，其实恒等于 1。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("COUNT(DISTINCT path) AS total").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopePath, from, to).
			Scan(&total).Error
	})
	return total, err
}

// RollupByPath 路径排行（浏览数降序、路径升序保证分页稳定）。
//
// offset 仅在「调用方没有游标」时使用（见 RollupByPathKeyset）；
// 有游标时走 keyset，深分页成本不随页码增长。
func (m *Model) RollupByPath(ctx context.Context, projectID string, from, to time.Time, offset, limit int) (rows []PathRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("path, SUM(views) AS views, SUM(visitors) AS visitors").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopePath, from, to).
			Group("path").Order("views DESC, path ASC").
			Offset(offset).Limit(limit).Scan(&rows).Error
	})
	return rows, err
}

// RollupByPathKeyset 路径排行的游标分页（审计 IDX-010）。
//
// 游标是「上一页最后一行的 (views, path)」：排序键 (views DESC, path ASC) 上，
// 下一页的条件是 views 更小、或 views 相同但 path 更大。这样数据库只需从索引
// 游标位置往后取 limit 行，不看前面的行 —— OFFSET 的成本随页码线性增长，游标不会。
//
// afterPath 为空串表示「从第一页开始」（第一页没有前驱，条件整体不生效）。
func (m *Model) RollupByPathKeyset(ctx context.Context, projectID string, from, to time.Time, afterViews int64, afterPath string, limit int) (rows []PathRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Table(tableNameDailyStats).
			Select("path, SUM(views) AS views, SUM(visitors) AS visitors").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopePath, from, to).
			Group("path")
		if afterPath != "" {
			q = q.Having("SUM(views) < ? OR (SUM(views) = ? AND path > ?)", afterViews, afterViews, afterPath)
		}
		return q.Order("views DESC, path ASC").Limit(limit).Scan(&rows).Error
	})
	return rows, err
}
