// Package buildmodel 构建任务队列的表访问单元（build_jobs，审计 DB-007）。
//
// 队列引擎选 PG 表而不是外部队列：构建任务需要**可见性**（后台能看队列深度与失败原因）
// 与**可纠错**（失败任务能重试、僵尸任务能回收）。这两件事在内存队列或 asynq 里
// 都要额外搭一套存储才能做到，而 build_jobs 这张表的形状本来就是为它设计的。
package buildmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameBuildJobs = "build_jobs"

// 允许的来源类型（与 DDL 的 build_jobs_source_type_check 逐字对应）。
//
// service 层也要有一份：数据库拒绝时的报错是约束名，到不了「看得懂的一句话」；
// 更重要的是入队前拒绝才不会有半条任务留在表里。
const (
	SourceTypePage         = "page"
	SourceTypePresentation = "presentation"
)

// 任务状态（与 DDL 的 CHECK 逐字对应）。
const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
	StatusSucceeded  = "succeeded"
)

// Entity build_jobs 表实体。
type Entity struct {
	ID             int64      `gorm:"column:id;type:bigint;primaryKey"`
	SourceType     string     `gorm:"column:source_type;type:text;not null"`
	SourceID       string     `gorm:"column:source_id;type:uuid;not null"`
	DraftVersion   int64      `gorm:"column:draft_version;not null"`
	BuildInputHash string     `gorm:"column:build_input_hash;type:text;not null"`
	Status         string     `gorm:"column:status;type:text;not null"`
	ArtifactID     *string    `gorm:"column:artifact_id;type:uuid"`
	ErrorMessage   *string    `gorm:"column:error_message;type:text"`
	CreateTime     time.Time  `gorm:"column:create_time;not null"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	CompletedAt    *time.Time `gorm:"column:completed_at"`
}

// TableName 表名。
func (Entity) TableName() string { return tableNameBuildJobs }

// Model build_jobs 表访问。
type Model struct{ db *gorm.DB }

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定本表的查询入口。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

// Enqueue 入队（幂等）。
//
// ON CONFLICT DO NOTHING 对着迁移 171 的部分唯一索引
// (source_type, source_id, build_input_hash) WHERE status='pending'：
// 依赖失效扇出会在一批里反复标记同一个页面，没有这层幂等，队列会被同一份工作填满。
// 返回 false 表示「同一份工作已经在队列里」。
func (m *Model) Enqueue(ctx context.Context, e *Entity) (created bool, err error) {
	res := m.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(e)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// claimSQL 取一条待办任务并置为 running。
//
// 三个要点，少一个都会出错：
//   - FOR UPDATE SKIP LOCKED：多个 worker 同时取任务时各拿各的，不排队、不重复；
//   - ORDER BY create_time ASC：先入先出，否则旧任务会被新任务无限插队；
//   - 同一语句里 SELECT + UPDATE：取与置位之间没有窗口，
//     否则两个 worker 可能都读到同一条 pending（看得到、抢不到的那种重复消费）。
const claimSQL = `UPDATE build_jobs
	SET status = 'running', started_at = now(), error_message = NULL
	WHERE id = (
	    SELECT id FROM build_jobs
	    WHERE status = 'pending'
	    ORDER BY create_time ASC
	    LIMIT 1
	    FOR UPDATE SKIP LOCKED
	)
	RETURNING id, source_type, source_id, draft_version, build_input_hash, status, create_time, started_at`

// Claim 取一条待办任务；队列为空时返回 (nil, nil)。
func (m *Model) Claim(ctx context.Context) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).Raw(claimSQL).Scan(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == 0 {
		return nil, nil
	}
	return &row, nil
}

// MarkSucceeded 标记任务成功（记录产物行 id）。
func (m *Model) MarkSucceeded(ctx context.Context, id int64, artifactID string, at time.Time) error {
	updates := map[string]any{"status": StatusSucceeded, "completed_at": at, "error_message": nil}
	if artifactID != "" {
		updates["artifact_id"] = artifactID
	}
	return m.DB(ctx).Where("id = ?", id).Updates(updates).Error
}

// MarkFailed 标记任务失败并记录原因（后台据此判断要不要重试）。
func (m *Model) MarkFailed(ctx context.Context, id int64, message string, at time.Time) error {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"status": StatusFailed, "error_message": message, "completed_at": at,
	}).Error
}

// MarkSuperseded 标记任务作废（同一目标已有更新的构建输入，这份工作没有意义了）。
func (m *Model) MarkSuperseded(ctx context.Context, id int64, at time.Time) error {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"status": StatusSuperseded, "completed_at": at,
	}).Error
}

// ReclaimStaleSQL 把超时未结束的 running 任务退回 pending。
//
// 没有这一步，「worker 崩在任务中途」会留下永远不动的 running 行：
// 既不会被消费，也看不出它已经死了 —— 队列会静默地少干活。
const reclaimStaleSQL = `UPDATE build_jobs
	SET status = 'pending', started_at = NULL,
	    error_message = COALESCE(error_message, 'worker 超时未结束，已退回队列')
	WHERE status = 'running' AND started_at IS NOT NULL AND started_at < ?`

// ReclaimStale 回收僵尸任务，返回退回队列的行数。
func (m *Model) ReclaimStale(ctx context.Context, before time.Time) (n int64, err error) {
	res := m.db.WithContext(ctx).Exec(reclaimStaleSQL, before)
	return res.RowsAffected, res.Error
}

// StatusCount 单个状态的行数。
type StatusCount struct {
	Status string `gorm:"column:status"`
	Count  int64  `gorm:"column:count"`
}

// CountByStatus 按状态统计队列深度。
func (m *Model) CountByStatus(ctx context.Context) (rows []StatusCount, err error) {
	err = m.DB(ctx).Select("status, COUNT(*) AS count").Group("status").Scan(&rows).Error
	return rows, err
}

// ListRecent 按状态列出最近的任务（空状态 = 全部）。
func (m *Model) ListRecent(ctx context.Context, status string, limit int) (list []*Entity, err error) {
	q := m.DB(ctx).Order("create_time DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if limit <= 0 {
		limit = 20
	}
	err = q.Limit(limit).Find(&list).Error
	return list, err
}

// Get 按 id 查询任务。
func (m *Model) Get(ctx context.Context, id string) (e *Entity, err error) {
	var row Entity
	if err = m.DB(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// RetryFailed 把失败任务退回队列（清空错误与完成时间）。
//
// 与「重新入队一条新任务」不同：重试保留原来的 draft_version 与 build_input_hash，
// 因此它仍然与部分唯一索引对齐 —— 若同一份工作已被重新排上，这次重试会被索引挡下，
// 不会变成两份并行的工作。
func (m *Model) RetryFailed(ctx context.Context, id int64) (ok bool, err error) {
	res := m.DB(ctx).Where("id = ? AND status = ?", id, StatusFailed).Updates(map[string]any{
		"status": StatusPending, "started_at": nil, "completed_at": nil, "error_message": nil,
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
