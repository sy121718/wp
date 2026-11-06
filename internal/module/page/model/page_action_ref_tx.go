package pagemodel

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// ListDraftDocumentsTx 在调用方事务内读取本工程未删除页面的草稿文档。
//
// 动作目标引用复核必须在删除方的业务事务、行锁之后执行：事务外的读取
// 与锁内快照不是同一视图，「刚才还没引用」不能证明「现在也没引用」。
// 只取复核所需列，不把草稿文档带进无关调用。
func (m *Model) ListDraftDocumentsTx(ctx context.Context, tx *gorm.DB, projectID string) (list []PageEntity, err error) {
	if tx == nil {
		return nil, ErrProjectRequired
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	err = tx.WithContext(ctx).Model(&PageEntity{}).
		Select("id", "project_id", "draft_path", "draft_document", "update_time").
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Order("update_time DESC, id DESC").
		Find(&list).Error
	return list, err
}

// ListPagesByDependencyTx 在调用方事务内读取编译产物登记了对某目标的页面。
//
// 与 ListPagesByDependency 同一口径，但共用调用方的锁内快照。
func (m *Model) ListPagesByDependencyTx(ctx context.Context, tx *gorm.DB, projectID, kind, key string) (list []DependencyRefRow, err error) {
	if tx == nil {
		return nil, ErrProjectRequired
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrProjectRequired
	}
	kind, key = strings.TrimSpace(kind), strings.TrimSpace(key)
	if kind == "" || key == "" {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&PageEntity{}).
		Select("pages.id AS id, "+staleTitleExpr+" AS title").
		Joins("JOIN "+tableNamePageDependencies+" d ON d.page_id = pages.id").
		Where("d.dependency_kind = ? AND d.dependency_key = ?", kind, key).
		Where("(d.artifact_id IN (pages.active_artifact_id, pages.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM page_publications WHERE page_id = pages.id UNION SELECT artifact_id FROM page_stagings WHERE page_id = pages.id))").
		Where("pages.project_id = ? AND pages.deleted_at IS NULL", projectID).
		Scan(&list).Error
	return list, err
}
