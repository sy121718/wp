package presentationmodel

import (
	"context"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
)

func (m *Model) ListTargetDependencyInstances(ctx context.Context, projectID, kind, key string) (ids []string, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		affected := presentationDepScope(tx, kind, key).Where("(d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM " + tableNamePresentationPublications + " WHERE presentation_id = p.id))")
		return tx.Model(&InstanceEntity{}).Where("project_id = ? AND deleted_at IS NULL", projectID).Where("id IN (?)", affected).Pluck("id", &ids).Error
	})
	return
}
