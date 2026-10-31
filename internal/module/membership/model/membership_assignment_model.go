package membershipmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SourceAuto   = "auto"
	SourceManual = "manual"
)

type AssignmentEntity struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	ProjectID  string    `gorm:"column:project_id"`
	UserID     uint64    `gorm:"column:user_id"`
	TierID     int64     `gorm:"column:tier_id"`
	Source     string    `gorm:"column:source"`
	AssignedAt time.Time `gorm:"column:assigned_at"`
	CreateTime time.Time `gorm:"column:create_time"`
	UpdateTime time.Time `gorm:"column:update_time"`
}

func (AssignmentEntity) TableName() string { return "membership_assignments" }

func (m *Model) GetAssignment(ctx context.Context, projectID string, userID uint64) (e *AssignmentEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		e, ierr = m.GetAssignmentTx(ctx, tx, projectID, userID)
		return ierr
	})
	return e, err
}

func (m *Model) GetAssignmentTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) (e *AssignmentEntity, err error) {
	var row AssignmentEntity
	if err = tx.WithContext(ctx).Model(&AssignmentEntity{}).
		Where("project_id = ? AND user_id = ?", projectID, userID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) UpsertManualTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) error {
	return manualUpsertStmt(ctx, tx, projectID, userID, tierID).Error
}

func manualUpsertStmt(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) *gorm.DB {
	now := time.Now()
	row := &AssignmentEntity{
		ProjectID: projectID, UserID: userID, TierID: tierID,
		Source: SourceManual, AssignedAt: now, CreateTime: now, UpdateTime: now,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"tier_id":     tierID,
			"source":      SourceManual,
			"assigned_at": now,
			"update_time": now,
		}),
	}).Model(&AssignmentEntity{}).Create(row)
}

func (m *Model) UpsertAutoTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) (changed bool, err error) {

	res := autoUpsertStmt(ctx, tx, projectID, userID, tierID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func autoUpsertStmt(ctx context.Context, tx *gorm.DB, projectID string, userID uint64, tierID int64) *gorm.DB {
	now := time.Now()
	row := &AssignmentEntity{
		ProjectID: projectID, UserID: userID, TierID: tierID,
		Source: SourceAuto, AssignedAt: now, CreateTime: now, UpdateTime: now,
	}
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"tier_id":     tierID,
			"source":      SourceAuto,
			"assigned_at": now,
			"update_time": now,
		}),

		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "membership_assignments.source <> ?", Vars: []interface{}{SourceManual}},
		}},
	}).Model(&AssignmentEntity{}).Create(row)
}

func (m *Model) UnlockManualTx(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) (int64, error) {
	res := unlockManualStmt(ctx, tx, projectID, userID)
	return res.RowsAffected, res.Error
}

func unlockManualStmt(ctx context.Context, tx *gorm.DB, projectID string, userID uint64) *gorm.DB {
	return tx.WithContext(ctx).Model(&AssignmentEntity{}).
		Where("project_id = ? AND user_id = ? AND source = ?", projectID, userID, SourceManual).
		Updates(map[string]any{"source": SourceAuto, "update_time": time.Now()})
}

func (m *Model) ListAssignmentsTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64, page, size int) (list []*AssignmentEntity, err error) {
	err = listAssignmentsStmt(ctx, tx, projectID, tierID, source, userID, page, size).Find(&list).Error
	return list, err
}

func listAssignmentsStmt(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64, page, size int) *gorm.DB {
	q := tx.WithContext(ctx).Model(&AssignmentEntity{}).Where("project_id = ?", projectID)
	q = applyAssignmentFilters(q, tierID, source, userID)
	if size > 0 {
		offset := (page - 1) * size
		if offset < 0 {
			offset = 0
		}
		q = q.Offset(offset).Limit(size)
	}
	return q.Order("tier_id DESC, user_id ASC")
}

func (m *Model) CountAssignmentsTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64) (total int64, err error) {
	err = countAssignmentsStmt(ctx, tx, projectID, tierID, source, userID).Count(&total).Error
	return total, err
}

func countAssignmentsStmt(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, source string, userID uint64) *gorm.DB {
	q := tx.WithContext(ctx).Model(&AssignmentEntity{}).Where("project_id = ?", projectID)
	return applyAssignmentFilters(q, tierID, source, userID)
}

func applyAssignmentFilters(q *gorm.DB, tierID int64, source string, userID uint64) *gorm.DB {
	if tierID > 0 {
		q = q.Where("tier_id = ?", tierID)
	}
	if source != "" {
		q = q.Where("source = ?", source)
	}
	if userID > 0 {
		q = q.Where("user_id = ?", userID)
	}
	return q
}

func (m *Model) ListAssignments(ctx context.Context, projectID string, tierID int64, source string, userID uint64, page, size int) (list []*AssignmentEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		list, ierr = m.ListAssignmentsTx(ctx, tx, projectID, tierID, source, userID, page, size)
		return ierr
	})
	return list, err
}

func (m *Model) CountAssignments(ctx context.Context, projectID string, tierID int64, source string, userID uint64) (total int64, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		total, ierr = m.CountAssignmentsTx(ctx, tx, projectID, tierID, source, userID)
		return ierr
	})
	return total, err
}

func (m *Model) CountByTierTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (int64, error) {
	var n int64
	err := tx.WithContext(ctx).Model(&AssignmentEntity{}).
		Where("project_id = ? AND tier_id = ?", projectID, tierID).
		Count(&n).Error
	return n, err
}
