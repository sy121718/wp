package buildmodel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/database"
)

const tableNameBuildJobs = "build_jobs"

const (
	SourceTypePage         = "page"
	SourceTypePresentation = "presentation"

	SourceTypeMenuStale = "menu_stale"
)

const (
	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSuperseded = "superseded"
	StatusFailed     = "failed"
	StatusSucceeded  = "succeeded"
)

const (
	IntentManual = "manual"

	IntentDependency = "dependency"
)

var ErrLeaseLost = fmt.Errorf("构建任务租约已失效（任务已被回收或合并）")

type Entity struct {
	ID         int64  `gorm:"column:id;primaryKey"`
	SourceType string `gorm:"column:source_type;not null"`
	SourceID   string `gorm:"column:source_id;not null"`

	ProjectID *string `gorm:"column:project_id"`

	Lang           string  `gorm:"column:lang;not null"`
	Intent         string  `gorm:"column:intent;not null"`
	DraftVersion   int64   `gorm:"column:draft_version;not null"`
	BuildInputHash string  `gorm:"column:build_input_hash;not null"`
	Status         string  `gorm:"column:status;not null"`
	ArtifactID     *string `gorm:"column:artifact_id"`
	ErrorMessage   *string `gorm:"column:error_message"`

	Attempt          int        `gorm:"column:attempt;not null"`
	LeaseToken       *string    `gorm:"column:lease_token"`
	LeaseExpiresTime *time.Time `gorm:"column:lease_expires_time"`

	InputVersion *int64 `gorm:"column:input_version"`

	ContextKey *string `gorm:"column:context_key"`

	CreateTime  time.Time  `gorm:"column:create_time;not null"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

var ErrTxRequired = fmt.Errorf("构建队列的事务内操作需要调用方已开启的真实事务句柄")

func (Entity) TableName() string { return tableNameBuildJobs }

type Model struct{ db *gorm.DB }

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

func (m *Model) Enqueue(ctx context.Context, e *Entity) (created bool, err error) {
	res := m.DB(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(e)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (m *Model) CreateTx(tx *gorm.DB, e *Entity) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	if e == nil {
		return fmt.Errorf("构建任务不能为空")
	}
	res := tx.Model(&Entity{}).Create(e)
	if res.Error != nil {
		if database.IsUniqueViolation(res.Error) {

			return fmt.Errorf("构建任务唯一键冲突（同一来源与输入版本已在队列中）: %w", res.Error)
		}
		return res.Error
	}
	return nil
}

func requireTx(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil || tx.Statement.ConnPool == nil {
		return ErrTxRequired
	}
	if err := tx.Error; err != nil {
		return err
	}
	switch conn := tx.Statement.ConnPool.(type) {
	case *sql.Tx:
		if conn == nil {
			return ErrTxRequired
		}
		return nil
	case *gorm.PreparedStmtTX:
		if conn == nil || conn.Tx == nil {
			return ErrTxRequired
		}
		return nil
	default:
		return ErrTxRequired
	}
}

func (m *Model) claimCandidates(ctx context.Context) *gorm.DB {
	return m.DB(ctx).
		Select("id").
		Where("status = ?", StatusPending).
		Where("NOT EXISTS (SELECT 1 FROM build_jobs r WHERE r.status = ? AND r.source_type = build_jobs.source_type AND r.source_id = build_jobs.source_id)", StatusRunning).
		Order("create_time ASC, id ASC").
		Limit(1).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
}

const claimMaxRetries = 3

func (m *Model) Claim(ctx context.Context, leaseTTL time.Duration) (e *Entity, err error) {
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("构建任务租约时长必须为正数: %s", leaseTTL)
	}
	var lastErr error
	for i := 0; i < claimMaxRetries; i++ {
		var row Entity

		res := m.db.WithContext(ctx).Model(&row).
			Where("id IN (?)", m.claimCandidates(ctx)).
			Clauses(clause.Returning{}).
			Updates(map[string]any{
				"status":     StatusRunning,
				"started_at": gorm.Expr("now()"),

				"lease_token": gorm.Expr("gen_random_uuid()"),

				"lease_expires_time": gorm.Expr("now() + make_interval(secs => ?)", leaseTTL.Seconds()),
				"attempt":            gorm.Expr("attempt + 1"),
				"error_message":      nil,
			})
		serr := res.Error
		if serr == nil {
			if row.ID == 0 {
				return nil, nil
			}
			return &row, nil
		}
		if !database.IsUniqueViolation(serr) {
			return nil, serr
		}
		lastErr = serr
	}
	return nil, lastErr
}

func (m *Model) leaseGuard(ctx context.Context, id int64, leaseToken string) (*gorm.DB, error) {
	if strings.TrimSpace(leaseToken) == "" {
		return nil, ErrLeaseLost
	}
	return m.DB(ctx).Where("id = ? AND status = ? AND lease_token = ?", id, StatusRunning, leaseToken), nil
}

func (m *Model) MarkSucceeded(ctx context.Context, id int64, leaseToken, artifactID string, at time.Time) error {
	q, err := m.leaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status": StatusSucceeded, "completed_at": at, "error_message": nil,

		"lease_token": nil, "lease_expires_time": nil,
	}
	if artifactID != "" {
		updates["artifact_id"] = artifactID
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (m *Model) MarkFailed(ctx context.Context, id int64, leaseToken, message string, at time.Time) error {
	q, err := m.leaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	res := q.Updates(map[string]any{
		"status": StatusFailed, "error_message": message, "completed_at": at,
		"lease_token": nil, "lease_expires_time": nil,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

func (m *Model) MarkSuperseded(ctx context.Context, id int64, leaseToken, reason string, at time.Time) error {
	q, err := m.leaseGuard(ctx, id, leaseToken)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"status": StatusSuperseded, "completed_at": at,
		"lease_token": nil, "lease_expires_time": nil,
	}
	if strings.TrimSpace(reason) != "" {
		updates["error_message"] = reason
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

const reclaimStaleSQL = `WITH stale AS (
	    SELECT b.id,
	           EXISTS (
	               SELECT 1 FROM build_jobs p
	                WHERE p.status = 'pending'
	                  AND p.source_type = b.source_type
	                  AND p.source_id = b.source_id
	                  AND p.build_input_hash = b.build_input_hash
	                  AND p.lang = b.lang
	                  AND p.intent = b.intent
	           ) AS has_pending
	      FROM build_jobs b
	     WHERE b.status = 'running'
	       AND b.lease_expires_time IS NOT NULL
	       AND b.lease_expires_time < ?::timestamptz
	     ORDER BY b.id
	     FOR UPDATE SKIP LOCKED
	)
	UPDATE build_jobs j
	   SET status = CASE WHEN s.has_pending THEN 'superseded' ELSE 'pending' END,
	       started_at = CASE WHEN s.has_pending THEN j.started_at ELSE NULL END,
	       lease_token = NULL,
	       lease_expires_time = NULL,
	       completed_at = CASE WHEN s.has_pending THEN ?::timestamptz ELSE NULL END,
	       error_message = CASE WHEN s.has_pending
	                            THEN COALESCE(j.error_message, '陈旧任务：同目标同构建输入的待办已在队列中，本次合并为 superseded')
	                            ELSE COALESCE(j.error_message, 'worker 租约到期未结束，已退回队列') END
	  FROM stale s
	 WHERE j.id = s.id
	RETURNING j.status`

type ReclaimResult struct {
	Reclaimed int64

	Merged int64
}

func (m *Model) ReclaimStale(ctx context.Context, at time.Time) (res ReclaimResult, err error) {
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		var rows []struct {
			Status string `gorm:"column:status"`
		}
		serr := m.db.WithContext(ctx).Raw(reclaimStaleSQL, at, at).Scan(&rows).Error
		if serr == nil {
			var out ReclaimResult
			for _, r := range rows {
				switch r.Status {
				case StatusPending:
					out.Reclaimed++
				case StatusSuperseded:
					out.Merged++
				}
			}
			return out, nil
		}
		if !database.IsUniqueViolation(serr) {
			return ReclaimResult{}, serr
		}
		lastErr = serr
	}
	return ReclaimResult{}, lastErr
}

type StatusCount struct {
	Status string `gorm:"column:status"`
	Count  int64  `gorm:"column:count"`
}

func (m *Model) CountByStatus(ctx context.Context, projectID string) (rows []StatusCount, err error) {
	q := m.DB(ctx).Select("status, COUNT(*) AS count")
	if pid := strings.TrimSpace(projectID); pid != "" {
		q = q.Where("project_id = ?", pid)
	}
	err = q.Group("status").Scan(&rows).Error
	return rows, err
}

func (m *Model) ListRecent(ctx context.Context, projectID, status string, limit int) (list []*Entity, err error) {
	q := m.DB(ctx).Order("create_time DESC")
	if pid := strings.TrimSpace(projectID); pid != "" {
		q = q.Where("project_id = ?", pid)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if limit <= 0 {
		limit = 20
	}
	err = q.Limit(limit).Find(&list).Error
	return list, err
}

func (m *Model) Get(ctx context.Context, id string) (e *Entity, err error) {
	var row Entity
	if err = m.DB(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) RetryFailed(ctx context.Context, id int64, projectID string) (ok bool, err error) {
	q := m.DB(ctx).Where("id = ? AND status = ?", id, StatusFailed)
	if pid := strings.TrimSpace(projectID); pid != "" {
		q = q.Where("project_id = ?", pid)
	}
	res := q.Updates(map[string]any{
		"status": StatusPending, "started_at": nil, "completed_at": nil, "error_message": nil,
		"lease_token": nil, "lease_expires_time": nil,
	})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
