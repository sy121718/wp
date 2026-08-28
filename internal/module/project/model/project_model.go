// Package projectmodel 实现 project 模块 projects 表持久化。
package projectmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const tableNameProjects = "projects"

// ProjectEntity 对应 projects 表。
type ProjectEntity struct {
	ID        string          `gorm:"column:id;type:uuid;primaryKey"`
	Name      string          `gorm:"column:name;type:text;not null"`
	Settings  json.RawMessage `gorm:"column:settings;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt time.Time       `gorm:"column:updated_at;not null"`
}

func (ProjectEntity) TableName() string { return tableNameProjects }

// Model projects 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewProjectModel 创建 Project Model。
func NewProjectModel(db *gorm.DB) *Model {
	return &Model{db: db}
}

// DB 返回已绑定 projects 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProjectEntity{})
}

// ThemeDB 返回已绑定 themes 表的 GORM 实例。
// 主题查询必须走本绑定：混用 DB(ctx)（已绑定 projects）会让 GORM
// 把 ThemeEntity 字段投影到 projects 表上，产生错误 SQL。
func (m *Model) ThemeDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ThemeEntity{})
}

// Create 新增工程。
func (m *Model) Create(ctx context.Context, e *ProjectEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// ListAll 按创建时间列出全部项目。
func (m *Model) ListAll(ctx context.Context) (list []ProjectEntity, err error) {
	err = m.DB(ctx).Order("created_at ASC").Find(&list).Error
	return list, err
}

// GetByID 按 ID 查询工程。
func (m *Model) GetByID(ctx context.Context, id string) (e *ProjectEntity, err error) {
	e = &ProjectEntity{}
	if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// Update 按 ID 更新工程名称与设置。
func (m *Model) Update(ctx context.Context, id, name string, settings json.RawMessage, updatedAt time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"name": name, "settings": settings, "updated_at": updatedAt,
	}).Error
}
