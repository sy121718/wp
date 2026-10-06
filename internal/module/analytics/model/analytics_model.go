// Package analyticsmodel analytics 模块表访问（page_views）。
package analyticsmodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNamePageViews = "page_views"

// PageViewEntity 对应 page_views 表（一次页面浏览一行）。
//
// 表里只有**派生后**的匿名值：会话标识、访客标识与 IP 全部先 hash 再落库，
// 原始 cookie 值与 IP 明文不进数据库 —— 计数不需要它们，存下来只会变成负担：
// 一旦落库就要为泄漏负责，而它们本来也推不回任何东西。
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
//
// 访客打点是全系统**唯一由浏览器写库**的路径（BIZ-8）：page_views 已启用 FORCE 策略
// （迁移 215），不设工程作用域时换非超级角色后这条 INSERT 会被 WITH CHECK 直接拒绝 ——
// 打点是有意静默失败的路径，缺 scope 的表现是「统计全为 0 且没有任何错误日志」。
func (m *Model) Insert(ctx context.Context, e *PageViewEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).Create(e).Error
	})
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

// CountByDay 按 UTC 日聚合（升序）。
//
// 为什么用 UTC 日界：产物由全球访客访问，服务端时区换来换去会让同一批数据
// 分到不同的日子里，报表对不上；UTC 是唯一与部署环境无关的口径。
func (m *Model) CountByDay(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("(viewed_at AT TIME ZONE 'UTC')::date AS day, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group("day").Order("day ASC").Scan(&rows).Error
	})
	return rows, err
}

// CountByHour 按 UTC 小时聚合（升序）。
//
// 与 CountByDay 逐项同口径（UTC 桶、同一个 PV/UV 表达式、同一个半开窗口），
// 只有 date_trunc 的单位不同 —— 两处口径分叉会让同一段数据在「按天」与「按小时」
// 两个粒度下加起来对不上。
//
// **`AT TIME ZONE 'UTC'` 写了两遍，第二遍不能省**：第一遍把 timestamptz 折成
// 「UTC 挂钟时间的 timestamp」（无时区），而 PG 驱动把无时区 timestamp 读成
// `time.Time` 时会**按本地时区贴位置** —— 于是同一行在本机（+08）读出来是
// `2026-10-05T01:00+08`，调用方再 `.UTC()` 就变成 `2026-10-04T17:00`，
// 桶键整整偏掉一个时区（图上多出一根「昨天 17 点」的柱子，而那条记录其实在
// 今天 1 点）。第二遍把它标回 timestamptz，驱动给出的才是正确时刻。
//
// `CountByDay` 没有这个问题：它 `::date` 返回的是 `date`，无时区但语义就是「某一天」，
// 驱动给 UTC 零点，`.UTC()` 是恒等变换。判据可以记成一句：**聚合列返回 `date` 且
// 只做 map 查找就没问题；返回 `timestamp` 一定要贴回 UTC**。这条由
// `TestViewBucketsReturnUTCWakeClock` 钉住（两种粒度都断言桶键的 UTC 挂钟，
// 并断言桶不越出查询窗口 —— 漂出去的那根柱子在图上会被当成新桶并进去）。
//
// **只走明细表，不读预聚合**：`page_views_daily` 的最小粒度就是天（rollup 每天跑一次），
// 小时粒度在它里面没有对应的行。代价是扫描原始明细，所以调用方要自己限制区间
// （概览页在 1~2 天的窗口内才用小时）。
func (m *Model) CountByHour(ctx context.Context, projectID string, from, to time.Time) (rows []DayRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("(date_trunc('hour', viewed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') AS day, "+colViews+", "+colVisitors).
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Group("day").Order("day ASC").Scan(&rows).Error
	})
	return rows, err
}

// CountPathTotal 窗口内出现过的不同路径数（按路径聚合的分页总数）。
func (m *Model) CountPathTotal(ctx context.Context, projectID string, from, to time.Time) (total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageViewEntity{}).
			Select("COUNT(DISTINCT path)").
			Where("project_id = ? AND viewed_at >= ? AND viewed_at < ?", projectID, from, to).
			Scan(&total).Error
	})
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

// CountByPath 按路径聚合（浏览数降序、路径升序保证分页稳定），支持分页。
//
// 排序里那第二列 path 不是为了好看：只按 views 排序时，值相同的行在两次查询里
// 顺序可能不同，翻页会出现重复与遗漏。
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
