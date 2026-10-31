package presentationmodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const publishRecordBatchSize = 500

const instancePublishFactColumns = "id, project_id, entity_type, entity_id, instance_role, url_path, stale, active_artifact_id"

func (m *Model) ListInstanceFactsByEntities(ctx context.Context, projectID, entityType string, entityIDs []string) (list []*InstanceEntity, err error) {
	ids := normalizeEntityIDs(entityIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	entityType = strings.TrimSpace(entityType)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		for start := 0; start < len(ids); start += publishRecordBatchSize {
			end := min(start+publishRecordBatchSize, len(ids))
			var batch []*InstanceEntity
			q := tx.WithContext(ctx).Model(&InstanceEntity{}).
				Select(instancePublishFactColumns).
				Where("project_id = ?", projectID)
			if entityType != "" {
				q = q.Where("entity_type = ?", entityType)
			}
			if err := q.Where("entity_id IN ?", ids[start:end]).
				Order("update_time DESC, id DESC").Find(&batch).Error; err != nil {
				return err
			}
			list = append(list, batch...)
		}
		return nil
	})
	return list, err
}

func normalizeEntityIDs(entityIDs []string) []string {

	out := make([]string, 0, len(entityIDs))
	seen := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (m *Model) ListPublicationsByInstances(ctx context.Context, instanceIDs []string) (list []PublicationEntity, err error) {
	list = []PublicationEntity{}
	for start := 0; start < len(instanceIDs); start += publishRecordBatchSize {
		end := min(start+publishRecordBatchSize, len(instanceIDs))
		var batch []PublicationEntity
		if err = m.PublicationDB(ctx).
			Where("presentation_id IN ?", instanceIDs[start:end]).
			Order("presentation_id ASC, lang ASC").Find(&batch).Error; err != nil {
			return nil, err
		}
		list = append(list, batch...)
	}
	return list, nil
}

type ArtifactVersionFact struct {
	ID           string
	Version      int64
	ArtifactHash string
}

func (m *Model) ListArtifactVersionFacts(ctx context.Context, artifactIDs []string) (list []ArtifactVersionFact, err error) {
	list = []ArtifactVersionFact{}
	for start := 0; start < len(artifactIDs); start += publishRecordBatchSize {
		end := min(start+publishRecordBatchSize, len(artifactIDs))
		var batch []ArtifactVersionFact
		if err = m.ArtifactDB(ctx).
			Select("id, version, artifact_hash").
			Where("id IN ?", artifactIDs[start:end]).
			Scan(&batch).Error; err != nil {
			return nil, err
		}
		list = append(list, batch...)
	}
	return list, nil
}
