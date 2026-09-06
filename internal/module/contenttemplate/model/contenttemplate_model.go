// Package contenttemplatemodel 实现 contenttemplate 模块两张表的持久化（0-A2）：
// content_templates（可编辑草稿）+ content_template_versions（不可变版本快照）。
package contenttemplatemodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const (
	tableNameContentTemplates        = "content_templates"
	tableNameContentTemplateVersions = "content_template_versions"
)

// TemplateEntity content_templates 表实体（模板草稿，可继续编辑）。
type TemplateEntity struct {
	ID            string          `gorm:"column:id;type:uuid;primaryKey"`
	Name          string          `gorm:"column:name;not null"`
	EntityType    string          `gorm:"column:entity_type;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion  int64           `gorm:"column:draft_version;not null"`
	CreatedAt     time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt     time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 表名。
func (TemplateEntity) TableName() string { return tableNameContentTemplates }

// VersionEntity content_template_versions 表实体（不可变版本快照）。
type VersionEntity struct {
	ID         string          `gorm:"column:id;type:uuid;primaryKey"`
	TemplateID string          `gorm:"column:template_id;type:uuid;not null"`
	Version    int64           `gorm:"column:version;not null"`
	Document   json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt  time.Time       `gorm:"column:created_at;not null"`
}

// TableName 表名。
func (VersionEntity) TableName() string { return tableNameContentTemplateVersions }

// Model contenttemplate 两张表的数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定模板表的查询入口。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&TemplateEntity{})
}

// DBVersion 绑定版本表的查询入口。
func (m *Model) DBVersion(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VersionEntity{})
}

// Create 新增模板草稿。
func (m *Model) Create(ctx context.Context, e *TemplateEntity) error {
	return m.DB(ctx).Create(e).Error
}

// Get 按 ID 查询模板。
func (m *Model) Get(ctx context.Context, id string) (e *TemplateEntity, err error) {
	var row TemplateEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 按 entity_type 列表（更新时间倒序；entity_type 为空时返回全部）。
func (m *Model) List(ctx context.Context, entityType string) (list []*TemplateEntity, err error) {
	q := m.db.WithContext(ctx).Order("updated_at DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	err = q.Find(&list).Error
	return list, err
}

// Save 更新草稿（draft_document + draft_version + updated_at）。
// 用 DB(ctx)（已绑定 Model）+ 显式 Where + Updates（避免 GORM Save
// 在已绑定 Model 下报 WHERE conditions required）。
func (m *Model) Save(ctx context.Context, e *TemplateEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"draft_document": e.DraftDocument,
		"draft_version":  e.DraftVersion,
		"updated_at":     e.UpdatedAt,
	}).Error
}

// CreateVersion 写入不可变版本快照。
func (m *Model) CreateVersion(ctx context.Context, v *VersionEntity) error {
	return m.DBVersion(ctx).Create(v).Error
}

// LatestVersion 取模板最新版本（version 降序首条）。
func (m *Model) LatestVersion(ctx context.Context, templateID string) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("template_id = ?", templateID).
		Order("version DESC").
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetVersion 按模板 ID + 版本号取指定版本。
func (m *Model) GetVersion(ctx context.Context, templateID string, version int64) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("template_id = ? AND version = ?", templateID, version).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}
