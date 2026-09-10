// Package contentmodel 实现 content 模块 contents 表持久化（0-A2）。
package contentmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const tableNameContents = "contents"

// Entity contents 表实体。
type Entity struct {
	ID         string          `gorm:"column:id;type:uuid;primaryKey"`
	EntityType string          `gorm:"column:entity_type;not null"`
	Slug       string          `gorm:"column:slug;not null"`
	Revision   int64           `gorm:"column:revision;not null"`
	Data       json.RawMessage `gorm:"column:data;type:jsonb;not null"`
	CreatedAt  time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt  time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 表名。
func (Entity) TableName() string { return tableNameContents }

// Model contents 表数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定本表的查询入口。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

// Create 新增实体。
func (m *Model) Create(ctx context.Context, e *Entity) error {
	return m.DB(ctx).Create(e).Error
}

// Get 按 ID 查询。
func (m *Model) Get(ctx context.Context, id string) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetBySlug 按类型+slug 查询。
func (m *Model) GetBySlug(ctx context.Context, entityType, slug string) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).
		Where("entity_type = ? AND slug = ?", entityType, slug).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 按类型分页列表（更新时间倒序）。
func (m *Model) List(ctx context.Context, entityType string, limit, offset int) (list []*Entity, err error) {
	q := m.db.WithContext(ctx).Order("updated_at DESC, id DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Find(&list).Error
	return list, err
}

// Save 覆盖更新（revision 由 service 层递增后传入）。
// 用 DB(ctx)（已绑定 Model）+ 显式 Where + Updates（避免 GORM Save
// 在已绑定 Model 下报 WHERE conditions required）。
func (m *Model) Save(ctx context.Context, e *Entity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"revision":   e.Revision,
		"data":       e.Data,
		"updated_at": e.UpdatedAt,
	}).Error
}

// Delete 删除实体。
func (m *Model) Delete(ctx context.Context, id string) error {
	return m.db.WithContext(ctx).Where("id = ?", id).Delete(&Entity{}).Error
}
