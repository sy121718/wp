package productmodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const maxProductSearchLimit = 50

func (m *Model) SearchPublished(ctx context.Context, projectID, keyword string, limit int) (list []*ProductEntity, err error) {
	projectID = strings.TrimSpace(projectID)
	keyword = strings.TrimSpace(keyword)
	if projectID == "" || keyword == "" {
		return nil, nil
	}
	if limit <= 0 || limit > maxProductSearchLimit {
		limit = maxProductSearchLimit
	}
	pattern := literalKeywordPattern(keyword)

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductEntity{}).
			Select("id, project_id, name, subtitle, slug, status, default_image, update_time").
			Where("project_id = ?", projectID).
			Where("status = ?", productStatusPublished).
			Where("(name ILIKE ? ESCAPE '\\' OR subtitle ILIKE ? ESCAPE '\\')", pattern, pattern).
			Order("update_time DESC, id DESC").
			Limit(limit).
			Find(&list).Error
	})
	return list, err
}

const productStatusPublished = "published"
