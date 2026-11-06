package pagemodel

import (
	"context"
	"strings"

	"gorm.io/gorm"

	json "go_wp/pkg/jsonx"
	"go_wp/pkg/rls"
)

// TemplateRefRow 是页面侧「某个页面仍引用模板」的事务内事实。
//
// Document 只在草稿 / 历史行上有值；依赖行只说明现行产物仍声明依赖。
type TemplateRefRow struct {
	PageID       string
	Title        string
	DraftPath    string
	Document     json.RawMessage
	FromRevision bool
	Version      int64
	FromDependency bool
}

// ListTemplateRefRowsTx 在调用方事务内反查页面草稿、历史版本与现行产物依赖里
// 仍绑定这些模板的行；工程作用域显式设置，不跨工程读取。
func (m *Model) ListTemplateRefRowsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) (rows []TemplateRefRow, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(templateIDs) == 0 {
		return nil, nil
	}
	list := strings.Join(templateIDs, ",")

	var drafts []TemplateRefRow
	if err = tx.WithContext(ctx).
		Table(tableNamePages).
		Select("id AS page_id, COALESCE(NULLIF(BTRIM(draft_document #>> '{settings,seo,title}'), ''), '') AS title, draft_path, draft_document AS document, false AS from_revision, 0::bigint AS version, false AS from_dependency").
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Where(pageStructureTemplateCond("draft_document"), list, list, list).
		Order("id ASC").Scan(&drafts).Error; err != nil {
		return nil, err
	}

	var revisions []TemplateRefRow
	if err = tx.WithContext(ctx).
		Table(tableNamePageRevisions+" AS r").
		Select("r.page_id, COALESCE(NULLIF(BTRIM(r.draft_document #>> '{settings,seo,title}'), ''), '') AS title, r.draft_path, r.draft_document AS document, true AS from_revision, r.version AS version, false AS from_dependency").
		Joins("JOIN "+tableNamePages+" AS p ON p.id = r.page_id").
		Where("p.project_id = ? AND p.deleted_at IS NULL", projectID).
		Where(pageStructureTemplateCond("r.draft_document"), list, list, list).
		Order("r.page_id ASC, r.version DESC").Scan(&revisions).Error; err != nil {
		return nil, err
	}

	var deps []TemplateRefRow
	if err = tx.WithContext(ctx).
		Table(tableNamePageDependencies+" AS d").
		Select("p.id AS page_id, COALESCE(NULLIF(BTRIM(p.draft_document #>> '{settings,seo,title}'), ''), '') AS title, p.draft_path, NULL::jsonb AS document, false AS from_revision, 0::bigint AS version, true AS from_dependency").
		Joins("JOIN "+tableNamePages+" AS p ON p.id = d.page_id").
		Where("p.project_id = ? AND p.deleted_at IS NULL", projectID).
		Where("d.dependency_kind = 'content_template' AND d.dependency_key = ANY(string_to_array(?, ','))", dependencyKeys(templateIDs)).
		Where("(d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM "+tableNamePagePublications+" WHERE page_id = p.id UNION SELECT artifact_id FROM "+tableNamePageStagings+" WHERE page_id = p.id))").
		Order("p.id ASC").Scan(&deps).Error; err != nil {
		return nil, err
	}

	rows = append(rows, drafts...)
	rows = append(rows, revisions...)
	rows = append(rows, deps...)
	return rows, nil
}

// pageStructureTemplateCond 用给定文档列构造结构模板绑定命中条件；
// 三个占位符都接收逗号分隔的模板 id 串。
func pageStructureTemplateCond(column string) string {
	return "(" + column + "->'settings'->'structure'->>'headerTemplateId' = ANY(string_to_array(?, ','))" +
		" OR " + column + "->'settings'->'structure'->>'footerTemplateId' = ANY(string_to_array(?, ','))" +
		" OR (jsonb_typeof(" + column + "->'settings'->'structure'->'slotTemplates') = 'object'" +
		" AND EXISTS (SELECT 1 FROM jsonb_each_text(" + column + "->'settings'->'structure'->'slotTemplates') AS kv(slot, tid)" +
		" WHERE kv.tid = ANY(string_to_array(?, ',')))))"
}

func dependencyKeys(templateIDs []string) string {
	out := make([]string, 0, len(templateIDs))
	for _, id := range templateIDs {
		out = append(out, "content_template:"+id)
	}
	return strings.Join(out, ",")
}
