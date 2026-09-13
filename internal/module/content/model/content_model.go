// Package contentmodel 实现 content 模块 contents 表持久化（0-A2）。
package contentmodel

import (
	"context"
	"encoding/json"
	"strings"
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

// maxSearchLimit 单次检索的硬上限。
//
// 检索的消费方是访问面片段（anonymous 请求）：调用方传 0（忘了传）或传一个很大的值时，
// 查询都不能退化成「扫全表再截断」。
const maxSearchLimit = 50

// SearchArticles 按关键词检索某类内容实体的标题与摘要，只读、限量。
//
// 两条必须一起成立的约束：
//
//  1. **全参数化**：关键词只经占位符传递，绝不拼进 SQL；
//  2. **LIKE 通配符转义**（与 media 模块同一手法）：关键词里的 % 与 _ 是字面量。
//     不转义时搜「50%」会变成「以 50 开头」、搜「a_b」会命中「axb」——
//     用户以为搜到了，其实是搜索在按另一套规则工作。ESCAPE '\' 与转义函数成对出现，
//     少一个都会让转义失效（反斜杠不再被当作转义符）。
//
// 标题与摘要在 contents.data（JSONB）里取：excerpt 是内容字段白名单的一员，
// 缺失时 data->>'excerpt' 为 NULL —— 只要标题命中仍然入选（OR，不是 AND）。
func (m *Model) SearchArticles(ctx context.Context, entityType, keyword string, limit int) (list []*Entity, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 || limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	pattern := "%" + escapeLikePattern(keyword) + "%"
	err = m.db.WithContext(ctx).
		Where("entity_type = ?", entityType).
		Where("(data->>'title' ILIKE ? ESCAPE '\\' OR data->>'excerpt' ILIKE ? ESCAPE '\\')", pattern, pattern).
		Order("updated_at DESC, id DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// escapeLikePattern 转义 LIKE 通配符（\ % _ 全部按字面量匹配，配合 ESCAPE '\'）。
//
// 顺序不能反：先转义反斜杠本身，否则后两步插入的反斜杠会被自己再转义一遍
// （"a\%" 会变成 "a\\\%" 这种多了一层的结果）。
func escapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
