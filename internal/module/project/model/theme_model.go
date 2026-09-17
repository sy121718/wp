// Theme theme_model.go — themes 表持久化:站点前端主题(多套,单套激活)。
package projectmodel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNameThemes = "themes"

// ThemeEntity 对应 themes 表:站点前端主题(颜色/字体/页眉页脚引用/布局参数)。
type ThemeEntity struct {
	ID        string          `gorm:"column:id;primaryKey"`
	ProjectID string          `gorm:"column:project_id;not null"`
	Name      string          `gorm:"column:name;not null"`
	Settings  json.RawMessage `gorm:"column:settings;type:jsonb;not null"`
	IsActive  bool            `gorm:"column:is_active;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

func (ThemeEntity) TableName() string { return tableNameThemes }

// CreateTheme 新增主题(同工程唯一名)。
// RLS（迁移 215）：themes 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
func (m *Model) CreateTheme(ctx context.Context, e *ThemeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Create(e).Error
	})
}

// ListThemes 列出工程全部主题(激活在前)。
func (m *Model) ListThemes(ctx context.Context, projectID string) (list []ThemeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).
			Order("is_active DESC, create_time ASC").Find(&list).Error
	})
	return list, err
}

// ListThemesByBlockID 列出**本工程内**绑定了指定全局块的全部主题。
// 用于全局块内容变更后的 stale 传播（调用方逐主题标记页面待重建）。
//
// projectID 非空时在工程作用域内查；为空沿用「不限工程」形态（换非超级角色后
// fail closed 返回空集 —— 表现为「改了块但页面不被标记」，列入 DB-009 剩余清单）。
func (m *Model) ListThemesByBlockID(ctx context.Context, projectID, blockID string) (list []ThemeEntity, err error) {
	apply := func(q *gorm.DB) *gorm.DB {
		// 覆盖两个历史字段与 slots 里的任意槽位：漏掉 slots 的表现是「改了公告条引用的块，
		// 页面不会被标记待重建」，站点上一直显示旧公告 —— 而且没有任何报错。
		return q.Where("settings->>'headerBlockId' = ? OR settings->>'footerBlockId' = ?"+
			" OR EXISTS (SELECT 1 FROM jsonb_each_text(COALESCE(settings->'slots', '{}'::jsonb)) AS e(k, v) WHERE e.v = ?)",
			blockID, blockID, blockID).Order("create_time ASC")
	}
	if strings.TrimSpace(projectID) == "" {
		err = apply(m.ThemeDB(ctx)).Find(&list).Error
		return list, err
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return apply(tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID)).Find(&list).Error
	})
	return list, err
}

// GetTheme 按 ID 查询主题。
//
// projectID 非空时在工程作用域内查；为空沿用「不限工程」形态 —— 不设 scope 的读取
// 在换非超级角色后 fail closed（0 行 → ErrRecordNotFound），方向是安全的；
// 要让它可用必须由调用方补 projectID（DB-009 剩余清单）。
func (m *Model) GetTheme(ctx context.Context, projectID, id string) (e *ThemeEntity, err error) {
	e = &ThemeEntity{}
	if strings.TrimSpace(projectID) == "" {
		if err = m.ThemeDB(ctx).Where("id = ?", id).First(e).Error; err != nil {
			return nil, err
		}
		return e, nil
	}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

// GetActiveTheme 取工程当前激活主题(激活优先,否则取最早创建的)。
func (m *Model) GetActiveTheme(ctx context.Context, projectID string) (e *ThemeEntity, err error) {
	e = &ThemeEntity{}
	if err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).
			Where("project_id = ?", projectID).
			Order("is_active DESC, create_time ASC").First(e).Error
	}); err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateTheme 更新主题设置与名称。
// projectID 非空时在工程作用域内写（越界写会被 WITH CHECK 直接拒绝，而不是静默改到别的工程）。
func (m *Model) UpdateTheme(ctx context.Context, projectID, id, name string, settings json.RawMessage, updatedAt time.Time) (err error) {
	if strings.TrimSpace(projectID) == "" {
		return m.ThemeDB(ctx).Where("id = ?", id).Updates(map[string]any{
			"name": name, "settings": settings, "update_time": updatedAt,
		}).Error
	}
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Where("id = ? AND project_id = ?", id, projectID).Updates(map[string]any{
			"name": name, "settings": settings, "update_time": updatedAt,
		}).Error
	})
}

// ActivateTheme 激活主题:同工程其余取消激活(事务保证单套激活)。
//
// 第二步必须检查受影响行数：若目标主题在 service 的 GetTheme 之后被并发删除，
// UPDATE 匹配 0 行且不报错，事务照样提交 —— 结果是整个工程 is_active 全 false，
// 而 API 回报「激活成功」（状态与响应不符）。返回 gorm.ErrRecordNotFound 由 service 判型。
func (m *Model) ActivateTheme(ctx context.Context, projectID, themeID string, updatedAt time.Time) (err error) {
	// 复用 InProjectScope 的事务：scope 必须先设，否则换角色后下面两条 UPDATE 都会
	// 因策略谓词为 NULL 而匹配 0 行 —— 其中第二条会被读成 ErrRecordNotFound（激活误报「主题不存在」）。
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		if err = tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).
			Update("is_active", false).Error; err != nil {
			return err
		}
		res := tx.Model(&ThemeEntity{}).Where("id = ? AND project_id = ?", themeID, projectID).
			Updates(map[string]any{"is_active": true, "update_time": updatedAt})
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
func (m *Model) DeleteTheme(ctx context.Context, projectID, id string) (rows int64, err error) {
	if strings.TrimSpace(projectID) == "" {
		res := m.ThemeDB(ctx).Where("id = ? AND is_active = false", id).Delete(&ThemeEntity{})
		return res.RowsAffected, res.Error
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		res := tx.Model(&ThemeEntity{}).
			Where("id = ? AND project_id = ? AND is_active = false", id, projectID).Delete(&ThemeEntity{})
		rows = res.RowsAffected
		return res.Error
	})
	return rows, err
}

// ExistsByName 判断工程下是否已存在同名主题（大小写不敏感）。
// excludeID 可选：排除指定主题自身（更新时复用）。
func (m *Model) ExistsByName(ctx context.Context, projectID, name string, excludeID ...string) (exists bool, err error) {
	var count int64
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.Model(&ThemeEntity{}).Where("project_id = ? AND LOWER(name) = LOWER(?)", projectID, name)
		if len(excludeID) > 0 && strings.TrimSpace(excludeID[0]) != "" {
			q = q.Where("id <> ?", excludeID[0])
		}
		return q.Count(&count).Error
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CountThemes 统计工程下主题数量（判断首建自动激活）。
func (m *Model) CountThemes(ctx context.Context, projectID string) (count int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).Count(&count).Error
	})
	return count, err
}
