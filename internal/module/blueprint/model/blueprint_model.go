// Package blueprintmodel 实现 blueprint 模块两张表的持久化（0-B）：
// blueprints（可编辑草稿）+ blueprint_versions（不可变版本快照）。
package blueprintmodel

import (
	"context"
	"encoding/json"
	"time"

	pageenums "go_wp/internal/module/page/enums"

	"gorm.io/gorm"
)

const (
	tableNameBlueprints        = "blueprints"
	tableNameBlueprintVersions = "blueprint_versions"
)

// IsValidKind Kind 是否在 Page 类型白名单内（与 migration 080 一致）。
func IsValidKind(kind string) bool {
	for _, k := range pageenums.PageKinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// PageKinds 返回全部 Page 类型（字典序，确定性输出）。
func PageKinds() []string { return pageenums.PageKinds() }

// BlueprintEntity blueprints 表实体（可编辑草稿）。
type BlueprintEntity struct {
	ID            string          `gorm:"column:id;type:uuid;primaryKey"`
	Name          string          `gorm:"column:name;not null"`
	Kind          string          `gorm:"column:kind;not null"`
	DraftDocument json.RawMessage `gorm:"column:draft_document;type:jsonb;not null"`
	DraftVersion  int64           `gorm:"column:draft_version;not null"`
	CreatedAt     time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt     time.Time       `gorm:"column:update_time;not null"`
}

// TableName 表名。
func (BlueprintEntity) TableName() string { return tableNameBlueprints }

// VersionEntity blueprint_versions 表实体（不可变版本快照）。
type VersionEntity struct {
	ID          string          `gorm:"column:id;type:uuid;primaryKey"`
	BlueprintID string          `gorm:"column:blueprint_id;type:uuid;not null"`
	Version     int64           `gorm:"column:version;not null"`
	Document    json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
}

// TableName 表名。
func (VersionEntity) TableName() string { return tableNameBlueprintVersions }

// Model blueprint 两张表的数据访问（Repository）。
type Model struct {
	db *gorm.DB
}

// NewModel 构造。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 绑定 blueprint 表的查询入口。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&BlueprintEntity{})
}

// DBVersion 绑定版本表的查询入口。
func (m *Model) DBVersion(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VersionEntity{})
}

// Transaction 透传事务（service 编排跨两张表的原子写入）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// Create 新增 Blueprint 草稿。
func (m *Model) Create(ctx context.Context, e *BlueprintEntity) error {
	return m.DB(ctx).Create(e).Error
}

// CreateWithVersion 同一事务内写草稿行 + 首个不可变版本行。
//
// 两步必须原子：草稿先落库而版本行失败时，该 Blueprint 永久没有
// LatestVersion，InitPageDocument 直接失败且无法自愈。
func (m *Model) CreateWithVersion(ctx context.Context, e *BlueprintEntity, v *VersionEntity) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		return tx.Create(v).Error
	})
}

// Get 按 ID 查询 Blueprint。
func (m *Model) Get(ctx context.Context, id string) (e *BlueprintEntity, err error) {
	var row BlueprintEntity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// List 按 kind 列表（更新时间倒序；kind 为空时返回全部）。
func (m *Model) List(ctx context.Context, kind string) (list []*BlueprintEntity, err error) {
	q := m.db.WithContext(ctx).Order("update_time DESC, id DESC")
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	err = q.Find(&list).Error
	return list, err
}

// Save 更新草稿（draft_document + draft_version + update_time）。
// 用 DB(ctx)（已绑定 Model）+ 显式 Where + Updates（避免 GORM Save
// 在已绑定 Model 下报 WHERE conditions required）。
func (m *Model) Save(ctx context.Context, e *BlueprintEntity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"draft_document": e.DraftDocument,
		"draft_version":  e.DraftVersion,
		"update_time":     e.UpdatedAt,
	}).Error
}

// CreateVersion 写入不可变版本快照。
func (m *Model) CreateVersion(ctx context.Context, v *VersionEntity) error {
	return m.DBVersion(ctx).Create(v).Error
}

// SaveWithVersion 同一事务内更新草稿 + 写新版本行。
//
// 原来 service 先 Save 再 CreateVersion：版本行失败时草稿已改而版本缺失，
// draft_version 也已在库里前移，重试会撞 (blueprint_id, version) 唯一约束。
func (m *Model) SaveWithVersion(ctx context.Context, e *BlueprintEntity, v *VersionEntity) error {
	return m.Transaction(ctx, func(tx *gorm.DB) error {
		if err := tx.Model(&BlueprintEntity{}).Where("id = ?", e.ID).Updates(map[string]any{
			"draft_document": e.DraftDocument,
			"draft_version":  e.DraftVersion,
			"update_time":     e.UpdatedAt,
		}).Error; err != nil {
			return err
		}
		return tx.Create(v).Error
	})
}

// LatestVersion 取 Blueprint 最新版本（version 降序首条）。
func (m *Model) LatestVersion(ctx context.Context, blueprintID string) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("blueprint_id = ?", blueprintID).
		Order("version DESC").
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// GetVersion 按 Blueprint ID + 版本号取指定版本。
func (m *Model) GetVersion(ctx context.Context, blueprintID string, version int64) (v *VersionEntity, err error) {
	var row VersionEntity
	if err = m.db.WithContext(ctx).
		Where("blueprint_id = ? AND version = ?", blueprintID, version).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// Delete 删除 Blueprint（blueprint_versions 经外键 ON DELETE CASCADE 级联清理）。
func (m *Model) Delete(ctx context.Context, id string) error {
	return m.db.WithContext(ctx).Where("id = ?", id).Delete(&BlueprintEntity{}).Error
}
