package analyticsmodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNamePageViews = "page_views"

type PageViewEntity struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	ProjectID    string    `gorm:"column:project_id;not null"`
	Path         string    `gorm:"column:path;not null"`
	Lang         string    `gorm:"column:lang;not null"`
	SessionID    string    `gorm:"column:session_id;not null"`
	VisitorHash  string    `gorm:"column:visitor_hash;not null"`
	ReferrerHost string    `gorm:"column:referrer_host;not null"`
	UAClass      string    `gorm:"column:ua_class;not null"`
	IPHash       string    `gorm:"column:ip_hash;not null"`
	ViewedAt     time.Time `gorm:"column:viewed_at;not null"`
}

func (PageViewEntity) TableName() string { return tableNamePageViews }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageViewEntity{})
}

func (m *Model) Insert(ctx context.Context, e *PageViewEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).Create(e).Error
	})
}

type DayRow struct {
	Day      time.Time
	Views    int64
	Visitors int64
}

type PathRow struct {
	Path     string
	Views    int64
	Visitors int64
}

const (
	colViews    = "COUNT(*) AS views"
	colVisitors = "COUNT(DISTINCT NULLIF(visitor_hash, '')) AS visitors"
)

func (m *Model) CountRange(ctx context.Context, projectID string, from, to time.Time) (views, visitors int64, err error) {
	var row struct {
		Views    int64
		Visitors int64
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select(colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Scan(&row).Error
	})
	if err != nil {
		return 0, 0, err
	}
	return row.Views, row.Visitors, nil
}

func (m *Model) CountByDay(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("(viewed_at AT TIME ZONE 'UTC')::date AS day, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group("day").Order("day ASC").Scan(&rows).Error
	})
	return rows, err
}

func (m *Model) CountByHour(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("(date_trunc('hour', viewed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS day, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group("day").Order("day ASC").Scan(&rows).Error
	})
	return rows, err
}

func (m *Model) CountPathTotal(ctx context.Context, projectID string, from, to time.Time) (total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("COUNT(DISTINCT path)").
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Scan(&total).Error
	})
	return total, err
}

func (m *Model) DeleteViewsBefore(ctx context.Context, projectID string, cutoff time.Time) (int64, error) {
	var res *gorm.DB
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res = tx.Model(&PageViewEntity{}).Where("project_id = ? AND viewed_at < ?", projectID, cutoff).Delete(&PageViewEntity{})
		return res.Error
	})
	if err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}

func (m *Model) CountByPath(ctx context.Context, projectID string, from, to time.Time, offset, limit int) (rows []PathRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("path, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group("path").Order("views DESC, path ASC").
			Offset(offset).Limit(limit).Scan(&rows).Error
	})
	return rows, err
}
