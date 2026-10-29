package pagemodel

import (
	"context"
	"go_wp/pkg/rls"
	"gorm.io/gorm"
	"strings"
)

// FindPagesByIDs 为重定向目标按工程批量查询未删除页面。
func (m *Model) FindPagesByIDs(ctx context.Context, projectID string, ids []string) (list []PageEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&PageEntity{}).Omit("draft_document").
			Where("project_id = ? AND id IN ? AND deleted_at IS NULL", projectID, ids).Find(&list).Error
	})
	return list, err
}
