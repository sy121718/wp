// Package analyticsmodel analytics 模块表访问（page_views）。
package analyticsmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const tableNamePageViews = "page_views"

// PageViewEntity 对应 page_views 表（一次页面浏览一行）。
//
// 表里只有**派生后**的匿名值：会话标识、访客标识与 IP 全部先 hash 再落库，
// 原始 cookie 值与 IP 明文不进数据库 —— 计数不需要它们，存下来只会变成负担：
// 一旦落库就要为泄漏负责，而它们本来也推不回任何东西。
type PageViewEntity struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	ProjectID    string    `gorm:"column:project_id;type:uuid;not null"`
	Path         string    `gorm:"column:path;not null"`
	Lang         string    `gorm:"column:lang;not null"`
	SessionID    string    `gorm:"column:session_id;not null"`
	VisitorHash  string    `gorm:"column:visitor_hash;not null"`
	ReferrerHost string    `gorm:"column:referrer_host;not null"`
	UAClass      string    `gorm:"column:ua_class;not null"`
	IPHash       string    `gorm:"column:ip_hash;not null"`
	ViewedAt     time.Time `gorm:"column:viewed_at;not null"`
}

// TableName 表名。
func (PageViewEntity) TableName() string { return tableNamePageViews }

// Model page_views 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewModel 创建 Model。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 page_views 表的 GORM 实例（只允许本 model 的仓储方法消费）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PageViewEntity{})
}

// Insert 写入一条页面浏览。
func (m *Model) Insert(ctx context.Context, e *PageViewEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// DayRow 按天聚合的一行（Day 为 UTC 日）。
type DayRow struct {
	Day      time.Time
	Views    int64
	Visitors int64
}

// PathRow 按路径聚合的一行。
type PathRow struct {
	Path     string
	Views    int64
	Visitors int64
}

// 聚合列：独立访客按 visitor_hash 去重，并先 NULLIF 掉空串 ——
// 没有 cookie 的访客（无 JS / 拒绝 cookie）会写空 hash，
// 不排除的话他们会被算成「一个额外的人」，而且是所有无 cookie 访客合成的那一个。
const (
	colViews    = "COUNT(*) AS views"
	colVisitors = "COUNT(DISTINCT NULLIF(visitor_hash, '')) AS visitors"
)

// CountRange 窗口内的总浏览数与独立访客数（窗口为 [from, to) 半开区间）。
func (m *Model) CountRange(ctx context.Context, projectID string, from, to time.Time) (views, visitors int64, err error) {
	var row struct {
		Views    int64
		Visitors int64
	}
	err = m.DB(ctx).
		Select(colViews+", "+colVisitors).
		Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
		Scan(&row).Error
	if err != nil {
		return 0, 0, err
	}
	return row.Views, row.Visitors, nil
}

// CountByDay 按 UTC 日聚合（升序）。
//
// 为什么用 UTC 日界：产物由全球访客访问，服务端时区换来换去会让同一批数据
// 分到不同的日子里，报表对不上；UTC 是唯一与部署环境无关的口径。
func (m *Model) CountByDay(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = m.DB(ctx).
		Select("(viewed_at AT TIME ZONE 'UTC')::date AS day, "+colViews+", "+colVisitors).
		Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
		Group("day").Order("day ASC").Scan(&rows).Error
	return rows, err
}

// CountPathTotal 窗口内出现过的不同路径数（按路径聚合的分页总数）。
func (m *Model) CountPathTotal(ctx context.Context, projectID string, from, to time.Time) (total int64, err error) {
	err = m.DB(ctx).
		Select("COUNT(DISTINCT path)").
		Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
		Scan(&total).Error
	return total, err
}

// RetentionPolicy 工程级访问明细保留策略。
type RetentionPolicy struct {
	ProjectID              string `gorm:"column:project_id"`
	AnalyticsRetentionDays int    `gorm:"column:analytics_retention_days"`
}

// ListRetentionPolicies 列出启用自动清理的工程（analytics_retention_days > 0）。
func (m *Model) ListRetentionPolicies(ctx context.Context) (list []RetentionPolicy, err error) {
	err = m.db.WithContext(ctx).
		Raw(`SELECT id AS project_id, analytics_retention_days FROM projects WHERE analytics_retention_days > 0`).
		Scan(&list).Error
	return list, err
}

// DeleteViewsBefore 删除某工程在 cutoff 之前的访问明细，返回删除行数。
func (m *Model) DeleteViewsBefore(ctx context.Context, projectID string, cutoff time.Time) (int64, error) {
	res := m.DB(ctx).Where("project_id = ? AND viewed_at < ?", projectID, cutoff).Delete(&PageViewEntity{})
	return res.RowsAffected, res.Error
}

// CountByPath 按路径聚合（浏览数降序、路径升序保证分页稳定），支持分页。
//
// 排序里那第二列 path 不是为了好看：只按 views 排序时，值相同的行在两次查询里
// 顺序可能不同，翻页会出现重复与遗漏。
func (m *Model) CountByPath(ctx context.Context, projectID string, from, to time.Time, offset, limit int) (rows []PathRow, err error) {
	err = m.DB(ctx).
		Select("path, "+colViews+", "+colVisitors).
		Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
		Group("path").Order("views DESC, path ASC").
		Offset(offset).Limit(limit).Scan(&rows).Error
	return rows, err
}
