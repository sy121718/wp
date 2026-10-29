package presentationmodel

import (
	"context"
	"time"

	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RetireInstanceTx 只清当前发布状态；历史文档、产物和尝试记录仍归原实例保留。
func (m *Model) RetireInstanceTx(ctx context.Context, tx *gorm.DB, projectID, id string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	var instance InstanceEntity
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND project_id = ?", id, projectID).First(&instance).Error; err != nil {
		return err
	}
	if instance.DeletedAt != nil {
		return nil
	}
	if err := tx.WithContext(ctx).Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{"active_artifact_id": nil, "staged_artifact_id": nil, "staged_snapshot_id": nil, "stale": false, "deleted_at": at, "update_time": at}).Error; err != nil {
		return err
	}
	if err := tx.WithContext(ctx).Where("presentation_id = ?", id).Delete(&PublicationEntity{}).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Where("presentation_id = ?", id).Delete(&DependencyEntity{}).Error
}
