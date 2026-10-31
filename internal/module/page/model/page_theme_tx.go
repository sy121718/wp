package pagemodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

func (m *Model) ReattachProjectPagesToThemeTx(ctx context.Context, tx *gorm.DB, projectID, themeID string) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		UpdateColumns(map[string]any{"theme_id": themeID, "update_time": time.Now().UTC()}).Error
}

func (m *Model) ListThemePageSnapshotsTx(ctx context.Context, tx *gorm.DB, projectID, themeID string) ([]ThemePageSnapshot, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	type row struct {
		ID         string
		ThemeOverr json.RawMessage `gorm:"column:theme_override"`
	}
	var raw []row
	if err := tx.WithContext(ctx).
		Table(tableNamePages).
		Select("id", "draft_document #> '{settings,themeOverride}' AS theme_override").
		Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	rows := make([]ThemePageSnapshot, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, ThemePageSnapshot{ID: r.ID, Override: r.ThemeOverr})
	}
	return rows, nil
}

func (m *Model) UpdateThemeSnapshotTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, themeJSON []byte, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		UpdateColumns(mergeInputChange(map[string]any{
			"draft_document": gorm.Expr("jsonb_set(draft_document, '{settings,theme}', ?, true)", themeJSON),
		}, at)).Error
}

func (m *Model) ListThemePageStructureSnapshotsTx(ctx context.Context, tx *gorm.DB, projectID, themeID string) ([]ThemePageStructureSnapshot, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	type row struct {
		ID            string
		PageStructure json.RawMessage `gorm:"column:page_structure"`
	}
	var raw []row
	if err := tx.WithContext(ctx).
		Table(tableNamePages).
		Select("id", "draft_document #> '{settings,structure}' AS page_structure").
		Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	rows := make([]ThemePageStructureSnapshot, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, ThemePageStructureSnapshot{ID: r.ID, Structure: r.PageStructure})
	}
	return rows, nil
}

func (m *Model) UpdateStructureSnapshotTx(ctx context.Context, tx *gorm.DB, projectID, pageID string, structureJSON []byte, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("id = ? AND project_id = ? AND deleted_at IS NULL", pageID, projectID).
		UpdateColumns(mergeInputChange(map[string]any{
			"draft_document": gorm.Expr("jsonb_set(draft_document, '{settings,structure}', ?, true)", structureJSON),
		}, at)).Error
}

func (m *Model) MarkStaleForThemeTx(ctx context.Context, tx *gorm.DB, projectID, themeID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&PageEntity{}).
		Where("project_id = ? AND theme_id = ? AND deleted_at IS NULL", projectID, themeID).
		UpdateColumns(map[string]any{
			"stale":         true,
			"input_version": gorm.Expr("input_version + 1"),
			"update_time":   at,
		}).Error
}
