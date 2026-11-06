package projectmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
)

const tableNameProjects = "projects"

type ProjectEntity struct {
	ID        string          `gorm:"column:id;primaryKey"`
	Name      string          `gorm:"column:name;not null"`
	Settings  json.RawMessage `gorm:"column:settings;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`

	AnalyticsRetentionDays int `gorm:"column:analytics_retention_days;->"`
}

func (ProjectEntity) TableName() string { return tableNameProjects }

type Model struct {
	db *gorm.DB
}

func NewProjectModel(db *gorm.DB) *Model {
	return &Model{db: db}
}

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProjectEntity{})
}

func (m *Model) ThemeDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ThemeEntity{})
}

// Transaction 提供一个工程作用域内的事务，供主题设置等写入口统一提交。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) Create(ctx context.Context, e *ProjectEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

func (m *Model) ListAll(ctx context.Context) (list []ProjectEntity, err error) {
	err = m.DB(ctx).Order("create_time ASC").Find(&list).Error
	return list, err
}

func (m *Model) GetByID(ctx context.Context, id string) (e *ProjectEntity, err error) {
	e = &ProjectEntity{}
	if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

func (m *Model) Update(ctx context.Context, id, name string, settings json.RawMessage, updatedAt time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"name": name, "settings": settings, "update_time": updatedAt,
	}).Error
}

func (m *Model) ListRetentionPolicies(ctx context.Context) (list []ProjectEntity, err error) {
	err = m.DB(ctx).
		Where("analytics_retention_days > 0").
		Order("create_time ASC, id ASC").
		Find(&list).Error
	return list, err
}
