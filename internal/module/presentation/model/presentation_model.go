// Package presentationmodel 实现 presentation 模块两张表持久化（0-A2）。
package presentationmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// InstanceEntity presentation_instances 表实体。
type InstanceEntity struct {
	ID                string    `gorm:"column:id;type:uuid;primaryKey"`
	EntityType        string    `gorm:"column:entity_type;not null"`
	EntityID          string    `gorm:"column:entity_id;type:uuid;not null"`
	URLPath           string    `gorm:"column:url_path;not null"`
	Status            string    `gorm:"column:status;not null"`
	CurrentSnapshotID *string   `gorm:"column:current_snapshot_id;type:uuid"`
	ArtifactHash      *string   `gorm:"column:artifact_hash"`
	CreatedAt         time.Time `gorm:"column:created_at;not null"`
	UpdatedAt         time.Time `gorm:"column:updated_at;not null"`
}

// TableName 表名。
func (InstanceEntity) TableName() string { return "presentation_instances" }

// SnapshotEntity document_snapshots 表实体。
type SnapshotEntity struct {
	ID                      string          `gorm:"column:id;type:uuid;primaryKey"`
	PresentationInstanceID  string          `gorm:"column:presentation_instance_id;type:uuid;not null"`
	SourceTemplateVersionID string          `gorm:"column:source_template_version_id;type:uuid;not null"`
	SourceEntityRevision    int64           `gorm:"column:source_entity_revision;not null"`
	Document                json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt               time.Time       `gorm:"column:created_at;not null"`
}

// TableName 表名。
func (SnapshotEntity) TableName() string { return "document_snapshots" }

// Model 两张表数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// InstanceDB 绑定实例表。
func (m *Model) InstanceDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&InstanceEntity{})
}

// SnapshotDB 绑定快照表。
func (m *Model) SnapshotDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&SnapshotEntity{})
}

// CreateInstance 新增实例。
func (m *Model) CreateInstance(ctx context.Context, e *InstanceEntity) error {
	return m.InstanceDB(ctx).Create(e).Error
}

// GetInstance 按 ID 查询。
func (m *Model) GetInstance(ctx context.Context, id string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetInstanceByEntity 按内容实体查询。
func (m *Model) GetInstanceByEntity(ctx context.Context, entityType, entityID string) (e *InstanceEntity, err error) {
	var row InstanceEntity
	if err = m.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListInstances 按类型列表。
func (m *Model) ListInstances(ctx context.Context, entityType string) (list []*InstanceEntity, err error) {
	q := m.db.WithContext(ctx).Order("updated_at DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	err = q.Find(&list).Error
	return list, err
}

// UpdateInstance 更新实例指针（snapshot_id + artifact_hash + status）。
func (m *Model) UpdateInstance(ctx context.Context, e *InstanceEntity) error {
	return m.InstanceDB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"current_snapshot_id": e.CurrentSnapshotID,
		"artifact_hash":       e.ArtifactHash,
		"status":              e.Status,
		"updated_at":          e.UpdatedAt,
	}).Error
}

// DeleteInstance 删除实例。
func (m *Model) DeleteInstance(ctx context.Context, id string) error {
	return m.db.WithContext(ctx).Where("id = ?", id).Delete(&InstanceEntity{}).Error
}

// CreateSnapshot 写快照。
func (m *Model) CreateSnapshot(ctx context.Context, e *SnapshotEntity) error {
	return m.SnapshotDB(ctx).Create(e).Error
}
