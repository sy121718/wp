package projectmodel

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	json "go_wp/pkg/jsonx"
	"go_wp/pkg/rls"
)

// ThemeTemplateRefRow 是主题设置里仍引用结构模板的事务内事实。
type ThemeTemplateRefRow struct {
	ThemeID   string
	ThemeName string
	Settings  json.RawMessage
}

// ListThemeTemplateRefRowsTx 在调用方事务内反查主题设置的结构模板绑定。
func (m *Model) ListThemeTemplateRefRowsTx(ctx context.Context, tx *gorm.DB, projectID string, templateIDs []string) (rows []ThemeTemplateRefRow, err error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	if len(templateIDs) == 0 {
		return nil, nil
	}
	list := strings.Join(templateIDs, ",")
	err = tx.WithContext(ctx).Model(&ThemeEntity{}).
		Select("id AS theme_id, name AS theme_name, settings").
		Where("project_id = ?", projectID).
		Where(`settings->>'headerTemplateId' = ANY(string_to_array(?, ','))
			OR settings->>'footerTemplateId' = ANY(string_to_array(?, ','))
			OR (jsonb_typeof(settings->'slotTemplates') = 'object'
			    AND EXISTS (SELECT 1 FROM jsonb_each_text(settings->'slotTemplates') AS kv(slot, tid)
			                WHERE kv.tid = ANY(string_to_array(?, ','))))`,
			list, list, list).
		Order("id ASC").Scan(&rows).Error
	return rows, err
}

// LockThemeTx 在工程作用域内锁定主题行并核对存在。
func (m *Model) LockThemeTx(ctx context.Context, tx *gorm.DB, projectID, themeID string) (*ThemeEntity, error) {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return nil, err
	}
	var row ThemeEntity
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND project_id = ?", themeID, projectID).Take(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateThemeTx 在调用方事务内更新主题名称与设置。
func (m *Model) UpdateThemeTx(ctx context.Context, tx *gorm.DB, projectID, id, name string, settings json.RawMessage, at time.Time) error {
	if err := rls.ScopeTx(tx, projectID); err != nil {
		return err
	}
	res := tx.WithContext(ctx).Model(&ThemeEntity{}).Where("id = ? AND project_id = ?", id, projectID).
		Updates(map[string]any{"name": name, "settings": settings, "update_time": at})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
