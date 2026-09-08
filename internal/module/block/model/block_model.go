// Package blockmodel 实现 blocks 表持久化：全局块（跨页面复用的结构片段）。
package blockmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const tableNameBlocks = "blocks"

// 块类型：普通块 / 页眉候选 / 页脚候选。
const (
	KindBlock  = "block"
	KindHeader = "header"
	KindFooter = "footer"
)

// DefaultCategory 块默认分类（自由分类体系，组织/筛选维度）。
const DefaultCategory = "general"

// BlockEntity 对应 blocks 表：全局块（组件树文档与页面 root 同构）。
type BlockEntity struct {
	ID        string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string          `gorm:"column:project_id;type:uuid;not null"`
	Name      string          `gorm:"column:name;type:text;not null"`
	Kind      string          `gorm:"column:kind;type:text;not null"`
	Category  string          `gorm:"column:category;type:text;not null;default:general"`
	Document  json.RawMessage `gorm:"column:document;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt time.Time       `gorm:"column:updated_at;not null"`
}

func (BlockEntity) TableName() string { return tableNameBlocks }

// Model blocks 表数据访问。
type Model struct {
	db *gorm.DB
}

// NewBlockModel 创建 Block Model。
func NewBlockModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 返回已绑定 blocks 表的 GORM 实例。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&BlockEntity{})
}

// Create 新增块。
func (m *Model) Create(ctx context.Context, e *BlockEntity) (err error) {
	return m.DB(ctx).Create(e).Error
}

// ListByProject 列出工程全部块（kind/category 可选过滤；类型序 + 创建序）。
func (m *Model) ListByProject(ctx context.Context, projectID, kind, category string) (list []BlockEntity, err error) {
	q := m.DB(ctx).Where("project_id = ?", projectID)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	err = q.Order("kind ASC, created_at ASC").Find(&list).Error
	return list, err
}

// ExistsByName 判断工程下是否已存在同名块（大小写不敏感，参数化单条查询）。
// 用于 Create 前判重：避免拉全量 Document(jsonb) 大字段后在内存 EqualFold。
// 注意：并发下同名仍可能穿透（需 DB 唯一索引兜底，见 service 层说明）。
func (m *Model) ExistsByName(ctx context.Context, projectID, name string) (exists bool, err error) {
	var count int64
	err = m.DB(ctx).
		Where("project_id = ? AND LOWER(name) = LOWER(?)", projectID, name).
		Count(&count).Error
	return count > 0, err
}

// GetByID 按 ID 查询块。
func (m *Model) GetByID(ctx context.Context, id string) (e *BlockEntity, err error) {
	e = &BlockEntity{}
	if err = m.DB(ctx).Where("id = ?", id).First(e).Error; err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateDocument 更新块名称、类型、分类与文档（覆盖式，编辑器整树保存）。
func (m *Model) UpdateDocument(ctx context.Context, id, name, kind, category string, document json.RawMessage, updatedAt time.Time) (err error) {
	return m.DB(ctx).Where("id = ?", id).Updates(map[string]any{
		"name": name, "kind": kind, "category": category, "document": document, "updated_at": updatedAt,
	}).Error
}

// Delete 删除块。
func (m *Model) Delete(ctx context.Context, id string) (err error) {
	return m.DB(ctx).Where("id = ?", id).Delete(&BlockEntity{}).Error
}
