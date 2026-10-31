package presentationmodel

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

const (
	RenderModeTemplate = "template"

	RenderModeDocument = "document"
)

func IsValidRenderMode(mode string) bool {
	return mode == RenderModeTemplate || mode == RenderModeDocument
}

func NormalizeRenderMode(mode string) string {
	if strings.TrimSpace(mode) == RenderModeDocument {
		return RenderModeDocument
	}
	return RenderModeTemplate
}

func IsDocumentMode(mode string) bool {
	return NormalizeRenderMode(mode) == RenderModeDocument
}

func (m *Model) UpdateInstanceModeTx(tx *gorm.DB, projectID, id, renderMode string,
	document json.RawMessage, at time.Time) error {
	if !IsValidRenderMode(renderMode) {
		return errors.New("invalid render mode")
	}
	if renderMode == RenderModeDocument && len(document) == 0 {
		return errors.New("document mode requires document")
	}
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	updates := map[string]any{
		"render_mode": renderMode,
		"update_time": at,
	}
	if renderMode == RenderModeDocument {
		updates["override_document"] = document
	} else {

		updates["override_document"] = nil
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).
		Updates(updates).Error
}

func (m *Model) ClearInstanceModeTx(tx *gorm.DB, projectID, id string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.Model(&InstanceEntity{}).Where("id = ? AND project_id = ?", id, projectID).
		Updates(map[string]any{
			"render_mode":       RenderModeTemplate,
			"override_document": nil,
			"update_time":       at,
		}).Error
}

func (m *Model) MarkStaleTemplateModeByDependency(ctx context.Context, projectID, kind, key string,
	at time.Time) (ids []string, err error) {
	if kind == "" || key == "" {
		return nil, nil
	}
	var rows []InstanceEntity
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		affected := presentationDepScope(tx, kind, key).
			Where("(COALESCE(p.render_mode, 'template') = 'template' OR d.dependency_key <> 'content_template:' || p.template_id::text)").
			Where("(d.artifact_id IN (p.active_artifact_id, p.staged_artifact_id) OR d.artifact_id IN (SELECT artifact_id FROM " + tableNamePresentationPublications + " WHERE presentation_id = p.id))")
		return tx.Model(&rows).
			Where("deleted_at IS NULL AND project_id = ?", projectID).
			Where("id IN (?)", affected).
			Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
			Updates(staleMarkUpdates(at)).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func (m *Model) CountInstancesByTemplate(ctx context.Context, projectID, templateID string) (
	templateMode, documentMode int64, err error) {
	if strings.TrimSpace(templateID) == "" {
		return 0, 0, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&InstanceEntity{}).
			Select("COUNT(*) FILTER (WHERE COALESCE(render_mode, 'template') = 'template') AS template_mode, "+
				"COUNT(*) FILTER (WHERE COALESCE(render_mode, 'template') = 'document') AS document_mode").
			Where("project_id = ? AND template_id = ? AND deleted_at IS NULL", projectID, templateID).
			Row().Scan(&templateMode, &documentMode)
	})
	return templateMode, documentMode, err
}

func (m *Model) ListSnapshots(ctx context.Context, instanceID string, limit int) (list []SnapshotEntity, err error) {
	if strings.TrimSpace(instanceID) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	err = m.db.WithContext(ctx).Model(&SnapshotEntity{}).
		Where("presentation_instance_id = ?", instanceID).
		Order("create_time DESC, id DESC").Limit(limit).Find(&list).Error
	return list, err
}
