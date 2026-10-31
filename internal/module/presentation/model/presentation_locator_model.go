package presentationmodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const maxLocatorEntityIDs = 50

func (m *Model) ListActiveURLPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (out map[string]string, err error) {
	out = map[string]string{}
	projectID = strings.TrimSpace(projectID)
	entityType = strings.TrimSpace(entityType)
	lang = strings.TrimSpace(lang)
	if projectID == "" || entityType == "" || lang == "" || len(entityIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(entityIDs))
	seen := make(map[string]bool, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		for start := 0; start < len(ids); start += maxLocatorEntityIDs {
			end := start + maxLocatorEntityIDs
			if end > len(ids) {
				end = len(ids)
			}

			var rows []struct {
				EntityID   string `gorm:"column:entity_id"`
				ActivePath string `gorm:"column:active_path"`
			}
			if qerr := tx.Table(tableNamePresentationInstances+" AS i").
				Select(`i.entity_id,
			COALESCE(pp.active_path, i.url_path) AS active_path`).
				Joins(`LEFT JOIN `+tableNamePresentationPublications+` AS pp
			ON pp.presentation_id = i.id AND pp.lang = ?`, lang).
				Where("i.project_id = ? AND i.entity_type = ? AND i.active_artifact_id IS NOT NULL AND i.entity_id IN ?",
					projectID, entityType, ids[start:end]).
				Find(&rows).Error; qerr != nil {
				return qerr
			}
			for _, row := range rows {
				if strings.TrimSpace(row.EntityID) == "" || strings.TrimSpace(row.ActivePath) == "" {
					continue
				}
				out[row.EntityID] = row.ActivePath
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
