package contenttemplatemodel

import (
	"context"
	"errors"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrDraftVersionConflict = errors.New("content template draft version conflict")

func (m *Model) LockDraftTx(ctx context.Context, tx *gorm.DB, projectID, id string) (*TemplateEntity, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	var row TemplateEntity
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ?", id, projectID).Take(&row).Error
	return &row, err
}

// CreateWithVersionTx 在调用方事务内创建模板与首个版本。
func (m *Model) CreateWithVersionTx(ctx context.Context, tx *gorm.DB, e *TemplateEntity, v *VersionEntity) error {
	if err := rls.ScopeTx(tx, e.ProjectID); err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Create(v).Error; err != nil {
		return err
	}
	res := tx.WithContext(ctx).Model(&TemplateEntity{}).Where("id = ?", e.ID).
		Updates(map[string]any{"current_version_id": v.ID, "update_time": e.UpdatedAt})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (m *Model) SaveWithVersionTx(ctx context.Context, tx *gorm.DB, projectID string, expectedVersion int64, v *VersionEntity, e *TemplateEntity) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	updated := tx.WithContext(ctx).Model(&TemplateEntity{}).
		Where("id = ? AND project_id = ? AND draft_version = ?", e.ID, projectID, expectedVersion).
		Updates(map[string]any{"draft_document": e.DraftDocument, "draft_version": e.DraftVersion, "current_version_id": e.CurrentVersionID, "update_time": e.UpdatedAt})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrDraftVersionConflict
	}
	return tx.WithContext(ctx).Create(v).Error
}
