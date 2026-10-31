package pagemodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type PagePathKindRow struct {
	Path string `gorm:"column:active_path"`
	Kind string `gorm:"column:kind"`
}

func (m *Model) KindsOfPaths(ctx context.Context, projectID string, paths []string) (kinds map[string]string, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	cleaned := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		cleaned = append(cleaned, p)
	}

	kinds = make(map[string]string, len(cleaned))
	if len(cleaned) == 0 {
		return kinds, nil
	}
	var rows []PagePathKindRow
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).
			Select("active_path", "kind").
			Where("project_id = ? AND deleted_at IS NULL AND active_path IN ?", projectID, cleaned).
			Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if strings.TrimSpace(r.Path) == "" {
			continue
		}
		kinds[r.Path] = r.Kind
	}
	return kinds, nil
}
