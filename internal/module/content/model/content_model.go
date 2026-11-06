package contentmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tableNameContents = "contents"

type Entity struct {
	ID         string          `gorm:"column:id;primaryKey"`
	EntityType string          `gorm:"column:entity_type;not null"`
	Slug       string          `gorm:"column:slug;not null"`
	Revision   int64           `gorm:"column:revision;not null"`
	Data       json.RawMessage `gorm:"column:data;type:jsonb;not null"`
	CreatedAt  time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time       `gorm:"column:update_time;not null"`
}

func (Entity) TableName() string { return tableNameContents }

type Model struct {
	db *gorm.DB
}

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&Entity{})
}

func (m *Model) Create(ctx context.Context, e *Entity) error {
	return m.DB(ctx).Create(e).Error
}

func (m *Model) Get(ctx context.Context, id string) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) GetBySlug(ctx context.Context, entityType, slug string) (e *Entity, err error) {
	var row Entity
	if err = m.db.WithContext(ctx).
		Where("entity_type = ? AND slug = ?", entityType, slug).
		First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *Model) contentListQuery(ctx context.Context, entityType, keyword string) *gorm.DB {
	q := m.db.WithContext(ctx).Model(&Entity{})
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if keyword != "" {
		pattern := "%" + database.EscapeLikePattern(keyword) + "%"
		q = q.Where("(title ILIKE ? ESCAPE '\\' OR slug ILIKE ? ESCAPE '\\')", pattern, pattern)
	}
	return q
}

func (m *Model) List(ctx context.Context, entityType, keyword string, limit, offset int) (list []*Entity, err error) {
	q := m.contentListQuery(ctx, entityType, keyword).Order("update_time DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Find(&list).Error
	return list, err
}

func (m *Model) Count(ctx context.Context, entityType, keyword string) (n int64, err error) {
	err = m.contentListQuery(ctx, entityType, keyword).Count(&n).Error
	return n, err
}

func (m *Model) Save(ctx context.Context, e *Entity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"revision":    e.Revision,
		"data":        e.Data,
		"update_time": e.UpdatedAt,
	}).Error
}

func (m *Model) Delete(ctx context.Context, id string) error {
	return m.db.WithContext(ctx).Where("id = ?", id).Delete(&Entity{}).Error
}

const maxSearchLimit = 50

func (m *Model) SearchArticles(ctx context.Context, entityType, keyword string, limit int) (list []*Entity, err error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}
	if limit <= 0 || limit > maxSearchLimit {
		limit = maxSearchLimit
	}
	pattern := "%" + database.EscapeLikePattern(keyword) + "%"

	err = m.db.WithContext(ctx).
		Where("entity_type = ?", entityType).
		Where("(title ILIKE ? ESCAPE '\\' OR data->>'excerpt' ILIKE ? ESCAPE '\\')", pattern, pattern).
		Order("update_time DESC, id DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

const collectionItemLimit = 100

type CollectionItem struct {
	ID        string          `gorm:"column:id"`
	Slug      string          `gorm:"column:slug"`
	Revision  int64           `gorm:"column:revision"`
	UpdatedAt time.Time       `gorm:"column:update_time"`
	Fields    json.RawMessage `gorm:"column:fields"`
}

func (m *Model) ListForCollection(ctx context.Context, entityType string, fields []string, filter map[string]string, offset, limit int) (list []*CollectionItem, err error) {
	if limit <= 0 {
		limit = collectionItemLimit
	}
	if offset < 0 {
		offset = 0
	}
	parts := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)*2+2)
	for _, f := range fields {

		parts = append(parts, "?::text, data->?::text")
		args = append(args, f, f)
	}
	projection := "id, slug, revision, update_time, jsonb_strip_nulls(jsonb_build_object(" +
		strings.Join(parts, ", ") + ")) AS fields"
	q := m.collectionQuery(ctx, entityType, filter).Select(projection, args...)

	err = q.Order("update_time DESC, id DESC").Limit(limit).Offset(offset).Find(&list).Error
	return list, err
}

func (m *Model) CountForCollection(ctx context.Context, entityType string, filter map[string]string) (total int64, err error) {
	err = m.collectionQuery(ctx, entityType, filter).Count(&total).Error
	return total, err
}

func (m *Model) collectionQuery(ctx context.Context, entityType string, filter map[string]string) *gorm.DB {
	q := m.db.WithContext(ctx).Table(tableNameContents)
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}

	keys := make([]string, 0, len(filter))
	for k := range filter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {

		q = q.Where("(data ->> ?::text) = ?::text", k, filter[k])
	}
	return q
}

// Transaction 提供一份事务句柄，供批量写整体提交或回滚。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// UpdateDataTx 在一个事务里锁定目标行、原子递增 revision 并写入新数据。
//
// 读-改-写必须在行锁内完成：先读 revision 再 Save 会让两个并发更新写入同一个
// revision，后提交者静默覆盖前者。「递增」由数据库表达式完成，不依赖读到的值。
func (m *Model) UpdateDataTx(ctx context.Context, id string, data json.RawMessage, updatedAt time.Time) (revision int64, rows int64, err error) {
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row Entity
		if lerr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).First(&row).Error; lerr != nil {
			return lerr
		}
		res := tx.Model(&Entity{}).Where("id = ?", id).Updates(map[string]any{
			"revision":    gorm.Expr("revision + 1"),
			"data":        data,
			"update_time": updatedAt,
		})
		if res.Error != nil {
			return res.Error
		}
		rows = res.RowsAffected
		return tx.Model(&Entity{}).Where("id = ?", id).
			Pluck("revision", &revision).Error
	})
	return revision, rows, err
}
