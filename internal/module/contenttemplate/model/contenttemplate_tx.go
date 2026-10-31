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
