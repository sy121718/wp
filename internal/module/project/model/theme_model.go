package projectmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNameThemes = "themes"

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

func (m *Model) CreateTheme(ctx context.Context, e *ThemeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Create(e).Error
	})
}

func (m *Model) ListThemes(ctx context.Context, projectID string) (list []ThemeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).
			Order("is_active DESC, create_time ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) ListThemesByBlockID(ctx context.Context, projectID, blockID string) (list []ThemeEntity, err error) {
	apply := func(q *gorm.DB) *gorm.DB {

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

func (m *Model) ActivateTheme(ctx context.Context, projectID, themeID string, updatedAt time.Time) (err error) {

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

func (m *Model) CountThemes(ctx context.Context, projectID string) (count int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.Model(&ThemeEntity{}).Where("project_id = ?", projectID).Count(&count).Error
	})
	return count, err
}
