package analyticsmodel

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const tableNameDailyStats = "page_views_daily"

const (
	ScopeAll = "all"

	ScopePath = "path"
)

type DailyStatEntity struct {
	ProjectID string    `gorm:"column:project_id;primaryKey"`
	Day       time.Time `gorm:"column:day;primaryKey"`
	Scope     string    `gorm:"column:scope;primaryKey"`
	Path      string    `gorm:"column:path;primaryKey"`
	Views     int64     `gorm:"column:views;not null"`
	Visitors  int64     `gorm:"column:visitors;not null"`
	RolledAt  time.Time `gorm:"column:rolled_at;not null"`
}

func (DailyStatEntity) TableName() string { return tableNameDailyStats }

const (
	dailyStatViewWindow      = "project_id = ? AND viewed_at >= ? AND viewed_at < ?"
	dailyStatViewCountSelect = "COUNT(*) AS views, COUNT(DISTINCT NULLIF(visitor_hash, '')) AS visitors"
)

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

func cleanupVanishedPaths(tx *gorm.DB, projectID string, day, from, to time.Time) error {
	views := PageViewEntity{}.TableName()
	return tx.Model(&DailyStatEntity{}).
		Where("project_id = ? AND day = ?::date AND scope = ?", projectID, day, ScopePath).
		Where("NOT EXISTS (SELECT 1 FROM "+views+" v WHERE v.project_id = "+tableNameDailyStats+".project_id"+
			" AND v.path = "+tableNameDailyStats+".path AND v.viewed_at >= ? AND v.viewed_at < ?)", from, to).
		Delete(&DailyStatEntity{}).Error
}

func (m *Model) RollupDay(ctx context.Context, projectID string, day, from, to time.Time) (err error) {

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

func (m *Model) ListProjectsWithViews(ctx context.Context, candidates []string) (ids []string, err error) {
	ids = make([]string, 0, len(candidates))
	for _, pid := range candidates {
		if cerr := ctx.Err(); cerr != nil {
			return ids, cerr
		}
		var n int64

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

func (m *Model) rollupScope(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Table(tableNameDailyStats)
}

func utcDayOf(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func (m *Model) EarliestViewDay(ctx context.Context, projectID string) (day time.Time, ok bool, err error) {

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

func (m *Model) CountRolledDays(ctx context.Context, projectID string, from, to time.Time) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
			Count(&n).Error
	})
	return n, err
}

func (m *Model) RollupTotals(ctx context.Context, projectID string, from, to time.Time) (views, visitors int64, err error) {

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

		if derr := tx.Model(&PageViewEntity{}).
			Select(colVisitors).
			Where(dailyStatViewWindow, projectID, from, to).
			Scan(&uv).Error; derr != nil {
			return derr
		}

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

func (m *Model) RollupByDay(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("day, SUM(views) AS views, SUM(visitors) AS visitors").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopeAll, from, to).
			Group("day").Having("SUM(views) > 0").Order("day ASC").Scan(&rows).Error
	})
	return rows, err
}

func (m *Model) RollupPathTotal(ctx context.Context, projectID string, from, to time.Time) (total int64, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Table(tableNameDailyStats).
			Select("COUNT(DISTINCT path) AS total").
			Where("project_id = ? AND scope = ? AND day >= ?::date AND day < ?::date", projectID, ScopePath, from, to).
			Scan(&total).Error
	})
	return total, err
}

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
