// Theme theme_model.go — themes 表持久化:站点前端主题(多套,单套激活)。
package projectmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const tableNameThemes = "themes"

// ThemeEntity 对应 themes 表:站点前端主题(颜色/字体/页眉页脚引用/布局参数)。
type ThemeEntity struct {
	ID        string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string          `gorm:"column:project_id;type:uuid;not null"`
	Name      string          `gorm:"column:name;type:text;not null"`
	Settings  json.RawMessage `gorm:"column:settings;type:jsonb;not null"`
	IsActive  bool            `gorm:"column:is_active;not null"`
	CreatedAt time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt time.Time       `gorm:"column:updated_at;not null"`
}

func (ThemeEntity) TableName() string { return tableNameThemes }

// CreateTheme 新增主题(同工程唯一名)。
func (m *Model) CreateTheme(ctx context.Context, e *ThemeEntity) (err error) {
	return m.ThemeDB(ctx).Create(e).Error
}

// ListThemes 列出工程全部主题(激活在前)。
func (m *Model) ListThemes(ctx context.Context, projectID string) (list []ThemeEntity, err error) {
	err = m.ThemeDB(ctx).Where("project_id = ?", projectID).
		Order("is_active DESC, created_at ASC").Find(&list).Error
	return list, err
}

// ListThemesByBlockID 列出页眉/页脚槽位绑定了指定全局块的全部主题。
// 用于全局块内容变更后的 stale 传播（调用方逐主题标记页面待重建）。
func (m *Model) ListThemesByBlockID(ctx context.Context, blockID string) (list []ThemeEntity, err error) {
	err = m.ThemeDB(ctx).
		Where("settings->>'headerBlockId' = ? OR settings->>'footerBlockId' = ?", blockID, blockID).
		Order("created_at ASC").Find(&list).Error
	return list, err
}

// GetTheme 按 ID 查询主题。
func (m *Model) GetTheme(ctx context.Context, id string) (e *ThemeEntity, err error) {
	e = &ThemeEntity{}
	if err = m.ThemeDB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// GetActiveTheme 取工程当前激活主题(激活优先,否则取最早创建的)。
func (m *Model) GetActiveTheme(ctx context.Context, projectID string) (e *ThemeEntity, err error) {
	e = &ThemeEntity{}
	if err = m.ThemeDB(ctx).
		Where("project_id = ?", projectID).
		Order("is_active DESC, created_at ASC").First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateTheme 更新主题设置与名称。
func (m *Model) UpdateTheme(ctx context.Context, id, name string, settings json.RawMessage, updatedAt time.Time) (err error) {
	return m.ThemeDB(ctx).Where("id = ?", id).Updates(map[string]any{
		"name": name, "settings": settings, "updated_at": updatedAt,
	}).Error
}

// ActivateTheme 激活主题:同工程其余取消激活(事务保证单套激活)。
func (m *Model) ActivateTheme(ctx context.Context, projectID, themeID string, updatedAt time.Time) (err error) {
	return m.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err = tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).
			Update("is_active", false).Error; err != nil {
			return err
		}
		return tx.Model(&ThemeEntity{}).Where("id = ? AND project_id = ?", themeID, projectID).
			Updates(map[string]any{"is_active": true, "updated_at": updatedAt}).Error
	})
}

// DeleteTheme 删除主题(非激活态,由 service 校验)。
func (m *Model) DeleteTheme(ctx context.Context, id string) (err error) {
	return m.ThemeDB(ctx).Where("id = ?", id).Delete(&ThemeEntity{}).Error
}
