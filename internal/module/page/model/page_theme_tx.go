package pagemodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// page_theme_tx.go — 主题换皮链的事务变体（DB-009 第二批）。
//
// 这些方法都在**调用方已开启的事务**上工作（换皮四步必须原子），所以作用域用
// rls.ScopeTx 设进那个事务，而不是另开一个：另开事务会让外层未提交的数据不可见，
// 同表写入还可能自锁（原子性被悄悄破坏）。pages 带 FORCE 策略，不设 app.project_id
// 时这些 UPDATE 在非超级角色下会匹配 0 行 —— 换主题看起来成功，页面快照与 stale
// 标记却一个都没落。

func (m *Model) ReattachProjectPagesToThemeTx(ctx context.Context, tx *gorm.DB, projectID, themeID string) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET theme_id = ?, update_time = ? WHERE project_id = ? AND deleted_at IS NULL",
		themeID, time.Now().UTC(), projectID,
	).Error
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
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,theme}', ?, true), update_time = ? WHERE id = ? AND project_id = ? AND deleted_at IS NULL",
		themeJSON, at, pageID, projectID,
	).Error
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
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET draft_document = jsonb_set(draft_document, '{settings,structure}', ?, true), update_time = ? WHERE id = ? AND project_id = ? AND deleted_at IS NULL",
		structureJSON, at, pageID, projectID,
	).Error
}

func (m *Model) MarkStaleForThemeTx(ctx context.Context, tx *gorm.DB, projectID, themeID string, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	return tx.WithContext(ctx).Exec(
		"UPDATE pages SET stale = true, update_time = ? WHERE project_id = ? AND theme_id = ? AND deleted_at IS NULL",
		at, projectID, themeID,
	).Error
}
