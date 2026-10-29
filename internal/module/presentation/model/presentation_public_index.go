package presentationmodel

import (
	"context"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"strings"
)

// ListPublicEntityPaths 只读真实单语言激活记录，不回退实例的候选 URL。
func (m *Model) ListPublicEntityPaths(ctx context.Context, projectID, entityType, lang string) (map[string]string, error) {
	out := map[string]string{}
	if projectID == "" || entityType == "" || lang == "" {
		return out, nil
	}
	err := rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		var rows []struct {
			EntityID   string
			ActivePath string
		}
		if err := tx.Table(tableNamePresentationInstances+" AS i").
			Select("i.entity_id, pp.active_path").
			Joins("JOIN "+tableNamePresentationPublications+" AS pp ON pp.presentation_id = i.id").
			Where("i.project_id = ? AND i.entity_type = ? AND pp.lang = ? AND pp.artifact_id IS NOT NULL AND pp.active_path <> ''", projectID, entityType, lang).
			Order("i.id ASC").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.EntityID != "" && strings.TrimSpace(row.ActivePath) != "" {
				out[row.EntityID] = row.ActivePath
			}
		}
		return nil
	})
	return out, err
}
