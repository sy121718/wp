package projectmodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const tableNameProjectLocales = "project_locales"

func (m *Model) withProjectScope(ctx context.Context, projectID string, fn func(tx *gorm.DB) error) error {
	return rls.InProjectScope(ctx, m.db, projectID, fn)
}

type LocaleEntity struct {
	ProjectID string    `gorm:"column:project_id;primaryKey"`
	Lang      string    `gorm:"column:lang;primaryKey"`
	SortOrder int       `gorm:"column:sort_order;not null"`
	IsDefault bool      `gorm:"column:is_default;not null"`
	Enabled   bool      `gorm:"column:enabled;not null"`
	CreatedAt time.Time `gorm:"column:create_time;not null"`
	UpdatedAt time.Time `gorm:"column:update_time;not null"`
}

func (LocaleEntity) TableName() string { return tableNameProjectLocales }

func (m *Model) LocaleDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&LocaleEntity{})
}

func (m *Model) ListLocales(ctx context.Context, projectID string) (list []LocaleEntity, err error) {
	err = m.withProjectScope(ctx, projectID, func(tx *gorm.DB) error {
		return tx.Model(&LocaleEntity{}).
			Where("project_id = ?", projectID).
			Order("is_default DESC, sort_order ASC, lang ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) ReplaceLocales(ctx context.Context, projectID string, rows []LocaleEntity) (err error) {
	return m.withProjectScope(ctx, projectID, func(tx *gorm.DB) error {
		if derr := tx.Where("project_id = ?", projectID).Delete(&LocaleEntity{}).Error; derr != nil {
			return derr
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}
