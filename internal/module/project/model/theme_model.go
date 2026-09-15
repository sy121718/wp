// Theme theme_model.go — themes 表持久化:站点前端主题(多套,单套激活)。
package projectmodel

import (
	"context"
	"encoding/json"
	"strings"
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

// ListThemesByBlockID 列出**任意结构槽位**绑定了指定全局块的全部主题。
// 用于全局块内容变更后的 stale 传播（调用方逐主题标记页面待重建）。
func (m *Model) ListThemesByBlockID(ctx context.Context, blockID string) (list []ThemeEntity, err error) {
	err = m.ThemeDB(ctx).
		// 覆盖两个历史字段与 slots 里的任意槽位：漏掉 slots 的表现是「改了公告条引用的块，
		// 页面不会被标记待重建」，站点上一直显示旧公告 —— 而且没有任何报错。
		Where("settings->>'headerBlockId' = ? OR settings->>'footerBlockId' = ?"+
			" OR EXISTS (SELECT 1 FROM jsonb_each_text(COALESCE(settings->'slots', '{}'::jsonb)) AS e(k, v) WHERE e.v = ?)",
			blockID, blockID, blockID).
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
//
// 第二步必须检查受影响行数：若目标主题在 service 的 GetTheme 之后被并发删除，
// UPDATE 匹配 0 行且不报错，事务照样提交 —— 结果是整个工程 is_active 全 false，
// 而 API 回报「激活成功」（状态与响应不符）。返回 gorm.ErrRecordNotFound 由 service 判型。
func (m *Model) ActivateTheme(ctx context.Context, projectID, themeID string, updatedAt time.Time) (err error) {
	return m.DB(ctx).Transaction(func(tx *gorm.DB) error {
		if err = tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).
			Update("is_active", false).Error; err != nil {
			return err
		}
		res := tx.Model(&ThemeEntity{}).Where("id = ? AND project_id = ?", themeID, projectID).
			Updates(map[string]any{"is_active": true, "updated_at": updatedAt})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// DeleteTheme 删除主题（原子守卫：仅当仍为非激活态时删除，规避 GetTheme 后并发激活的 TOCTOU）。
// 返回受影响行数：rows=0 表示目标不存在或已变为激活态，由 service 判型。
func (m *Model) DeleteTheme(ctx context.Context, id string) (rows int64, err error) {
	res := m.ThemeDB(ctx).Where("id = ? AND is_active = false", id).Delete(&ThemeEntity{})
	return res.RowsAffected, res.Error
}

// ExistsByName 判断工程下是否已存在同名主题（大小写不敏感）。
// excludeID 可选：排除指定主题自身（更新时复用）。
func (m *Model) ExistsByName(ctx context.Context, projectID, name string, excludeID ...string) (exists bool, err error) {
	q := m.ThemeDB(ctx).Where("project_id = ? AND LOWER(name) = LOWER(?)", projectID, name)
	if len(excludeID) > 0 && strings.TrimSpace(excludeID[0]) != "" {
		q = q.Where("id <> ?", excludeID[0])
	}
	var count int64
	if err = q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CountThemes 统计工程下主题数量（判断首建自动激活）。
func (m *Model) CountThemes(ctx context.Context, projectID string) (count int64, err error) {
	err = m.ThemeDB(ctx).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}
