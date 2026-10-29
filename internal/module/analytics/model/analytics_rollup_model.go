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
	"gorm.io/gorm/clause"

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

// 聚合口径：与明细逐字一致（COUNT(*) 与 COUNT(DISTINCT NULLIF(visitor_hash,”))）。
// 一致性是这套方案的前提 —— 汇总与明细算的是两套公式的话，
// 「切到汇总之后数字变了」就会变成一个没人能解释的现象。
//
// all 行即使当天 0 访问也要写一行 views=0（而不是只在有明细时才写）：
// 否则「这一天汇总过没有」无从判断，查询侧就只能保守地退回明细（性能白优化）。
const (
	dailyStatViewWindow      = "project_id = ? AND viewed_at >= ? AND viewed_at < ?"
	dailyStatViewCountSelect = "COUNT(*) AS views, COUNT(DISTINCT NULLIF(visitor_hash, '')) AS visitors"
)

// upsertDailyStats 按四列主键幂等覆盖（ON CONFLICT DO UPDATE）。
//
// 汇总的正确性靠「全量重算 + 幂等覆盖」而不是增量累加：某一天被重算多少次，
// 结果都等于从明细重新算一遍的值。
func upsertDailyStats(tx *gorm.DB, rows []DailyStatEntity) error {
	if len(rows) == 0 {
		return nil
	}
	return tx.Model(&DailyStatEntity{}).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "project_id"}, {Name: "day"}, {Name: "scope"}, {Name: "path"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"views", "visitors", "rolled_at"}),
		}).
		Create(&rows).Error
}

// cleanupVanishedPaths 清理重算后已不存在的路径行。
//
// 明细被清理（保留期到期）后，汇总里对应的路径行若不删就会永远留着，
// 路径排行里出现一个点不开、也查不到明细的幽灵条目。
func cleanupVanishedPaths(tx *gorm.DB, projectID string, day, from, to time.Time) error {
	views := PageViewEntity{}.TableName()
	return tx.Model(&DailyStatEntity{}).
		Where("project_id = ? AND day = ?::date AND scope = ?", projectID, day, ScopePath).
		Where("NOT EXISTS (SELECT 1 FROM "+views+" v WHERE v.project_id = "+tableNameDailyStats+".project_id"+
			" AND v.path = "+tableNameDailyStats+".path AND v.viewed_at >= ? AND v.viewed_at < ?)", from, to).
		Delete(&DailyStatEntity{}).Error
}

// RollupDay 重算某一天（UTC 日界）的汇总，from/to 是该日的半开区间。
//
// 幂等：同一天跑多少次，结果都等于明细的当前状态。
func (m *Model) RollupDay(ctx context.Context, projectID string, day, from, to time.Time) (err error) {
	// 三条语句读写的是 page_views_daily 与 page_views（两张都带策略），必须在同一
	// 工程作用域里跑：汇总写入若被策略挡下，水位（LastRolledDay）却推进了 —— 会留下
	// 一整段「已汇总但数字为 0」的历史，且没有任何报错。
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var all struct {
			Views    int64 `gorm:"column:views"`
			Visitors int64 `gorm:"column:visitors"`
		}
		if qerr := tx.Model(&PageViewEntity{}).
			Select(dailyStatViewCountSelect).
			Where(dailyStatViewWindow, projectID, from, to).
			Scan(&all).Error; qerr != nil {
			return qerr
		}
		var paths []struct {
			Path     string `gorm:"column:path"`
			Views    int64  `gorm:"column:views"`
			Visitors int64  `gorm:"column:visitors"`
		}
		if qerr := tx.Model(&PageViewEntity{}).
			Select("path, "+dailyStatViewCountSelect).
			Where(dailyStatViewWindow, projectID, from, to).
			Group("path").
			Scan(&paths).Error; qerr != nil {
			return qerr
		}
		// rolled_at 由应用侧统一取一次：同一次重算的所有行标记同一时刻。
		at := time.Now().UTC()
		rows := make([]DailyStatEntity, 0, len(paths)+1)
		rows = append(rows, DailyStatEntity{
			ProjectID: projectID, Day: day, Scope: ScopeAll, Path: "",
			Views: all.Views, Visitors: all.Visitors, RolledAt: at,
		})
		for _, p := range paths {
			rows = append(rows, DailyStatEntity{
				ProjectID: projectID, Day: day, Scope: ScopePath, Path: p.Path,
				Views: p.Views, Visitors: p.Visitors, RolledAt: at,
			})
		}
		if uerr := upsertDailyStats(tx, rows); uerr != nil {
			return uerr
		}
		return cleanupVanishedPaths(tx, projectID, day, from, to)
	})
}

// ListProjectsWithViews 在给定候选工程里筛出**有访问明细**的工程（汇总任务的输入）。
//
// 候选清单由调用方传入（来自 project 契约）：`projects` 表的所有权在 project 模块，
// 本 model 只查 page_views（自己那张表）。
//
// 取「有访问的工程」而不是全部工程：没有访问的工程不需要汇总行，
// 汇总任务也不该被空工程拖着跑。
//
// 逐工程在作用域内探测：page_views 带 FORCE 策略、谓词读会话变量 app.project_id，
// 没有作用域时直查恒返回空集（fail closed 且不报错）。失效形态是最难发现的一种 ——
// 汇总任务照常每小时跑，RollupRecent 返回 projects=0，日志里一行异常都没有，
// 站点只是「历史窗口的数字一直比明细少」。
func (m *Model) ListProjectsWithViews(ctx context.Context, candidates []string) (ids []string, err error) {
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

// RollupTotals 历史窗口的合计（PV / UV）。
//
// **PV 取汇总，UV 取明细去重**（PUB-07 / B07）。这不是折中，是唯一正确的口径：
//
//   - PV 可以相加：一天 10 次 + 一天 20 次 = 区间 30 次。汇总表成立的前提就是这个。
//   - UV **不能相加**：`page_views_daily.visitors` 是**按天**去重的计数，同一个人
//     周一与周三各来一次时两天各记 1，SUM 得 2 —— 而区间内的真实 UV 是 1。
//     SUM(按天 UV) 得到的其实是"人次"，把它显示成"访客数"会让回访率高的站
//     看起来访客接近 PV，而实际上只是同一小撮人在反复来。
//
// 所以 UV 回到明细表做 COUNT(DISTINCT visitor_hash)：去重必须发生在**区间**这一层，
// 而只有明细里有逐条的 visitor_hash。代价是一次明细扫描 —— 但它只在"窗口完全在过去"
// 这条路径上发生，而那条路径本来就已经用了汇总表（省掉的正是这次扫描之外的开销）；
// 给一个错的数字省一次查询，不是优化。
//
// 按天维度（RollupByDay）仍读汇总表：那里的每一天各自去重，SUM 的问题不存在。
func (m *Model) RollupTotals(ctx context.Context, projectID string, from, to time.Time) (views, visitors int64, err error) {
	// 两条查询用两个变量：Scan 会复用同一个结构体，第二次扫描把第一次的字段清零 ——
	// 实测症状是 PV 恒为 0（第一次算出 4，第二次只填 visitors，views 变 0）。
	var agg struct {
		Views int64 `gorm:"column:views"`
	}
	var uv struct {
		Visitors int64 `gorm:"column:visitors"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if derr := tx.Table(tableNameDailyStats).
			Select("COALESCE(SUM(views), 0) AS views").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
			Scan(&agg).Error; derr != nil {
			return derr
		}
		// UV：优先在明细上做**区间级**去重（正确口径，见本函数头注释）。
		if derr := tx.Model(&PageViewEntity{}).
			Select(colVisitors).
			Where(dailyStatViewWindow, projectID, from, to).
			Scan(&uv).Error; derr != nil {
			return derr
		}
		// 明细被保留期清理过的那段历史：明细里一条都不剩，但汇总行还在。
		// 此时退回 SUM(按天 UV) —— 它给出的是"人次"而不是人数，但那比 0 诚实：
		// 0 会让"三年前的流量"看起来像零访问，而 SUM 至少与"按天 UV 之和"自洽。
		// 两种口径都记不住的场景不存在：明细在就用明细，明细不在就用汇总。
		if uv.Visitors == 0 && agg.Views > 0 {
			return tx.Table(tableNameDailyStats).
				Select("COALESCE(SUM(visitors), 0) AS visitors").
				Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
				Scan(&uv).Error
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return agg.Views, uv.Visitors, nil
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
