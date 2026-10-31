package pagemodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type DependencyRefRow struct {
	ID    string `gorm:"column:id"`
	Title string `gorm:"column:title"`
}

func (m *Model) ListPagesByDependency(ctx context.Context, projectID, kind, key string) (list []DependencyRefRow, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	kind, key = strings.TrimSpace(kind), strings.TrimSpace(key)
	if kind == "" || key == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		return tx.Model(&PageEntity{}).
			Select("pages.id AS id, "+staleTitleExpr+" AS title").
			Joins("JOIN "+tableNamePageDependencies+" d ON d.page_id = pages.id").
			Where("d.dependency_kind = ? AND d.dependency_key = ?", kind, key).
			Where("(d.artifact_id IN (pages.active_artifact_id, pages.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM page_publications WHERE page_id = pages.id UNION SELECT artifact_id FROM page_stagings WHERE page_id = pages.id))").
			Where("pages.project_id = ? AND pages.deleted_at IS NULL", projectID).
			Scan(&list).Error
	})
	return list, err
}
