package presentationmodel

import (
	"context"

	"gorm.io/gorm"

	json "go_wp/pkg/jsonx"
	"go_wp/pkg/rls"
)

// TemplateRefRow 是自动发布实例侧仍引用模板 / 模板版本的事务内事实。
//
// 只读本模块表：模板 id 由调用方给出，本查询只回答「哪些实例 / 快照仍引用它」。
type TemplateRefRow struct {
	TemplateID string
	InstanceID string
	EntityType string
	EntityID   string
	URLPath    string
	Document   json.RawMessage
	// FromSnapshot 说明这条事实来自 document_snapshots 的模板版本外键。
	FromSnapshot bool
}

// ListInstanceTemplateRefRowsTx 在调用方事务内反查：
//
//   - instances.template_id 命中模板；
//   - document_snapshots.source_template_version_id 命中任一模板版本。
//
// 实例独立文档里的结构槽绑定由调用方按 Document 解析，不在 SQL 里夸模块拼表。
func (m *Model) ListInstanceTemplateRefRowsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs, versionIDs []string) (rows []TemplateRefRow, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(templateIDs) > 0 {
		var direct []TemplateRefRow
		if err = tx.WithContext(ctx).
			Table(tableNamePresentationInstances+" AS i").
			Select("i.template_id, i.id AS instance_id, i.entity_type, i.entity_id, i.url_path, i.override_document AS document, false AS from_snapshot").
			Where("i.project_id = ? AND i.deleted_at IS NULL AND i.template_id IN ?", projectID, templateIDs).
			Order("i.id ASC").Scan(&direct).Error; err != nil {
			return nil, err
		}
		rows = append(rows, direct...)
	}
	if len(versionIDs) > 0 {
		var snaps []TemplateRefRow
		if err = tx.WithContext(ctx).
			Table(tableNameDocumentSnapshots+" AS s").
			Select("'' AS template_id, i.id AS instance_id, i.entity_type, i.entity_id, i.url_path, s.document, true AS from_snapshot").
			Joins("JOIN "+tableNamePresentationInstances+" AS i ON i.id = s.presentation_instance_id").
			Where("i.project_id = ? AND i.deleted_at IS NULL AND s.source_template_version_id IN ?", projectID, versionIDs).
			Order("i.id ASC").Scan(&snaps).Error; err != nil {
			return nil, err
		}
		rows = append(rows, snaps...)
	}
	return rows, nil
}
