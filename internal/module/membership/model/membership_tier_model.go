package membershipmodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type TierEntity struct {
	ID        int64  `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id"`
	Name      string `gorm:"column:name"`

	SortOrder int `gorm:"column:sort_order"`

	ThresholdAmount int64      `gorm:"column:threshold_amount"`
	IsDefault       bool       `gorm:"column:is_default"`
	Remark          string     `gorm:"column:remark"`
	CreateTime      time.Time  `gorm:"column:create_time"`
	UpdateTime      time.Time  `gorm:"column:update_time"`
	DeletedAt       *time.Time `gorm:"column:deleted_at"`
}

func (TierEntity) TableName() string { return "membership_tiers" }

func (m *Model) ListTiers(ctx context.Context, projectID string) (list []*TierEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		list, ierr = m.ListTiersTx(ctx, tx, projectID)
		return ierr
	})
	return list, err
}

func (m *Model) ListTiersTx(ctx context.Context, tx *gorm.DB, projectID string) (list []*TierEntity, err error) {
	err = tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Order("sort_order DESC, id ASC").
		Find(&list).Error
	return list, err
}

func (m *Model) GetTier(ctx context.Context, projectID string, tierID int64) (e *TierEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		e, ierr = m.GetTierTx(ctx, tx, projectID, tierID)
		return ierr
	})
	return e, err
}

func (m *Model) GetTierTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (e *TierEntity, err error) {
	var row TierEntity
	if err = tx.WithContext(ctx).Model(&TierEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", tierID, projectID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) GetDefaultTier(ctx context.Context, projectID string) (e *TierEntity, err error) {
	err = m.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		var ierr error
		e, ierr = m.GetDefaultTierTx(ctx, tx, projectID)
		return ierr
	})
	return e, err
}

func (m *Model) GetDefaultTierTx(ctx context.Context, tx *gorm.DB, projectID string) (e *TierEntity, err error) {
	var row TierEntity
	if err = tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND is_default AND deleted_at IS NULL", projectID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) CreateTierTx(ctx context.Context, tx *gorm.DB, e *TierEntity) error {
	return tx.WithContext(ctx).Model(&TierEntity{}).Create(e).Error
}

func (m *Model) UpdateTierFieldsTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64, fields map[string]any) (int64, error) {
	if len(fields) == 0 {
		return 0, nil
	}
	fields["update_time"] = time.Now()
	res := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", tierID, projectID).
		Updates(fields)
	return res.RowsAffected, res.Error
}

func (m *Model) SoftDeleteTierTx(ctx context.Context, tx *gorm.DB, projectID string, tierID int64) (int64, error) {
	res := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", tierID, projectID).
		Updates(map[string]any{"deleted_at": time.Now(), "update_time": time.Now()})
	return res.RowsAffected, res.Error
}

func (m *Model) CountByThresholdTx(ctx context.Context, tx *gorm.DB, projectID string, threshold int64, excludeID int64) (int64, error) {
	var n int64
	q := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND threshold_amount = ? AND is_default = FALSE AND deleted_at IS NULL", projectID, threshold)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}

func (m *Model) ClearDefaultTx(ctx context.Context, tx *gorm.DB, projectID string, excludeID int64) (int64, error) {
	res := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND is_default AND deleted_at IS NULL AND id <> ?", projectID, excludeID).
		Updates(map[string]any{"is_default": false, "update_time": time.Now()})
	return res.RowsAffected, res.Error
}

func (m *Model) CountByNameTx(ctx context.Context, tx *gorm.DB, projectID, name string, excludeID int64) (int64, error) {
	var n int64
	q := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND name = ? AND deleted_at IS NULL", projectID, name)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}

func (m *Model) CountDefaultTx(ctx context.Context, tx *gorm.DB, projectID string, excludeID int64) (int64, error) {
	var n int64
	q := tx.WithContext(ctx).Model(&TierEntity{}).
		Where("project_id = ? AND is_default AND deleted_at IS NULL", projectID)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&n).Error
	return n, err
}
