package pagemodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

func (m *Model) ReattachProjectPagesToThemeTx(ctx context.Context, tx *gorm.DB, projectID, themeID string) error {
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET theme_id = ?, update_time = ? WHERE project_id = ? AND deleted_at IS NULL",
		themeID, time.Now().UTC(), projectID,
	).Error
}

func (m *Model) ListThemePageSnapshotsTx(ctx context.Context, tx *gorm.DB, themeID string) ([]ThemePageSnapshot, error) {
	type row struct {
		ID         string
		ThemeOverr json.RawMessage `gorm:"column:theme_override"`
	}
	var raw []row
	if err := tx.WithContext(ctx).
		Table(tableNamePages).
		Select("id", "draft_document #> '{settings,themeOverride}' AS theme_override").
		Where("theme_id = ? AND deleted_at IS NULL", themeID).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	rows := make([]ThemePageSnapshot, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, ThemePageSnapshot{ID: r.ID, Override: r.ThemeOverr})
	}
	return rows, nil
}

func (m *Model) UpdateThemeSnapshotTx(ctx context.Context, tx *gorm.DB, pageID string, themeJSON []byte, at time.Time) error {
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,theme}', ?, true), update_time = ? WHERE id = ? AND deleted_at IS NULL",
		themeJSON, at, pageID,
	).Error
}

func (m *Model) ListThemePageStructureSnapshotsTx(ctx context.Context, tx *gorm.DB, themeID string) ([]ThemePageStructureSnapshot, error) {
	type row struct {
		ID            string
		PageStructure json.RawMessage `gorm:"column:page_structure"`
	}
	var raw []row
	if err := tx.WithContext(ctx).
		Table(tableNamePages).
		Select("id", "draft_document #> '{settings,structure}' AS page_structure").
		Where("theme_id = ? AND deleted_at IS NULL", themeID).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	rows := make([]ThemePageStructureSnapshot, 0, len(raw))
	for _, r := range raw {
		rows = append(rows, ThemePageStructureSnapshot{ID: r.ID, Structure: r.PageStructure})
	}
	return rows, nil
}

func (m *Model) UpdateStructureSnapshotTx(ctx context.Context, tx *gorm.DB, pageID string, structureJSON []byte, at time.Time) error {
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,structure}', ?, true), update_time = ? WHERE id = ? AND deleted_at IS NULL",
		structureJSON, at, pageID,
	).Error
}

func (m *Model) MarkStaleForThemeTx(ctx context.Context, tx *gorm.DB, themeID string, at time.Time) error {
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET stale = true, update_time = ? WHERE theme_id = ? AND deleted_at IS NULL",
		at, themeID,
	).Error
}
