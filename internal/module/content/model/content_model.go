// Package contentmodel 实现 content 模块 contents 表持久化（0-A2）。
package contentmodel

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"go_wp/pkg/database"

	"gorm.io/gorm"
)

const tableNameContents = "contents"

// Entity contents 表实体。
type Entity struct {
	ID         string          `gorm:"column:id;primaryKey"`
	EntityType string          `gorm:"column:entity_type;not null"`
	Slug       string          `gorm:"column:slug;not null"`
	Revision   int64           `gorm:"column:revision;not null"`
	Data       json.RawMessage `gorm:"column:data;type:jsonb;not null"`
	CreatedAt  time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt  time.Time       `gorm:"column:update_time;not null"`
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
	q := m.db.WithContext(ctx).Order("update_time DESC, id DESC")
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Find(&list).Error
	return list, err
}

// Count 按类型统计条数（**与 List 同一份过滤条件**：entityType）。
//
// 分页要算总页数就得在 SQL 侧数 —— 把「已取回的一页」当成全部，正是「翻不过第 N 页
// 还以为到底了」的成因。过滤条件与 List 保持一致：一侧漏掉类型过滤时，总数会把别的
// 内容类型也算进来、分页条凭空多出几页，而两条 SQL 各自看都对。
func (m *Model) Count(ctx context.Context, entityType string) (n int64, err error) {
	q := m.db.WithContext(ctx).Model(&Entity{})
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	err = q.Count(&n).Error
	return n, err
}

// Save 覆盖更新（revision 由 service 层递增后传入）。
// 用 DB(ctx)（已绑定 Model）+ 显式 Where + Updates（避免 GORM Save
// 在已绑定 Model 下报 WHERE conditions required）。
func (m *Model) Save(ctx context.Context, e *Entity) error {
	return m.DB(ctx).Where("id = ?", e.ID).Updates(map[string]any{
		"revision":    e.Revision,
		"data":        e.Data,
		"update_time": e.UpdatedAt,
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
	pattern := "%" + database.EscapeLikePattern(keyword) + "%"
	// title 用**生成列**（迁移 169，审计 DB-024）而不是 data->>'title'：
	// JSONB 表达式没有索引，`data->>'title' ILIKE '%x%'` 只能全表扫；
	// 生成列上有 trgm 索引，OR 的左边能走它。生成列由数据库从 data 自动维护，
	// 写入路径因此一行都不用改，也不可能出现「title 与 data 不一致」。
	// excerpt 仍在 JSONB 里（不进列表与集合投影，也没有索引需求）。
	err = m.db.WithContext(ctx).
		Where("entity_type = ?", entityType).
		Where("(title ILIKE ? ESCAPE '\\' OR data->>'excerpt' ILIKE ? ESCAPE '\\')", pattern, pattern).
		Order("update_time DESC, id DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// collectionItemLimit 集合源单次取数上限（与该集合源改造前的 100 条口径一致：
// 组件集合渲染不分页，超出的部分本就不输出）。
const collectionItemLimit = 100

// CollectionItem 集合项投影（审计 PERF-008）。
//
// 与 Entity 的差别是「取什么」：Entity 拉整行（含 data 里的正文全文），
// CollectionItem 只取集合渲染要用的字段，且字段值由 SQL 就地拼成 JSON 对象 ——
// 正文因此从不经过应用进程。
type CollectionItem struct {
	ID        string          `gorm:"column:id"`
	Slug      string          `gorm:"column:slug"`
	Revision  int64           `gorm:"column:revision"`
	UpdatedAt time.Time       `gorm:"column:update_time"`
	Fields    json.RawMessage `gorm:"column:fields"`
}

// ListForCollection 集合源取数：列投影 + 筛选下推 + 确定性排序。
//
// fields 是集合项要暴露的字段（调用方从白名单取，见 contentcontract.CollectionFieldWhitelist）；
// filter 是等值过滤，下推成 `(data ->> 'k') = 'v'`。键名与值一律以参数进入 SQL：
// 键名虽来自白名单常量，仍然走占位符 —— 「拼 SQL」这件事一旦在一处开了口子，
// 下一处就很难守住。
//
// 投影用 jsonb_strip_nulls 包住：jsonb_build_object 对缺失字段会写出 null，
// 而改造前「键不存在」与「键存在但为 null」是两种状态，组件靠它区分「没有这个字段」
// 与「字段是空的」（见 builder/source 的取值访问器语义），不能合并成一种。
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
		// 显式 ::text：jsonb_build_object 是 any 变参函数，不给类型时 PG 无法推断
		// 参数类型（42P18 could not determine data type of parameter）。
		parts = append(parts, "?::text, data->?::text")
		args = append(args, f, f)
	}
	projection := "id, slug, revision, update_time, jsonb_strip_nulls(jsonb_build_object(" +
		strings.Join(parts, ", ") + ")) AS fields"
	q := m.collectionQuery(ctx, entityType, filter).Select(projection, args...)
	// offset 由调用方给（审计 PERF-019）：构建期恒为 0（只取第一屏），片段期按页码算。
	err = q.Order("update_time DESC, id DESC").Limit(limit).Offset(offset).Find(&list).Error
	return list, err
}

// CountForCollection 满足同一组过滤条件的总条数（审计 PERF-019）。
//
// 分页要算总页数就得知道总量，而总量只能在 SQL 侧数 —— 把「已取回的一页」当成全部，
// 正是这条审计要修的那个问题（翻不过第 N 页还以为到底了）。
// 过滤条件与 ListForCollection 共用 collectionQuery，两条查询不会各自漂移。
func (m *Model) CountForCollection(ctx context.Context, entityType string, filter map[string]string) (total int64, err error) {
	err = m.collectionQuery(ctx, entityType, filter).Count(&total).Error
	return total, err
}

// collectionQuery 集合源查询的公共部分：表 + 实体类型 + 白名单等值过滤。
//
// 抽出来是为一件事：List 与 Count 必须用**完全一致**的过滤条件 —— 一边改了另一边没改，
// 表现是「总页数按旧条件算」，翻到最后一页才发现少了几条，而两条 SQL 各自看都对。
func (m *Model) collectionQuery(ctx context.Context, entityType string, filter map[string]string) *gorm.DB {
	q := m.db.WithContext(ctx).Table(tableNameContents)
	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	// 按键名排序后拼条件：map 迭代顺序不定，不排序会让同一次查询的参数顺序随机变化
	// （结果一样，但 prepared statement 缓存会白白多出几个变体，慢查询日志也不好比对）。
	keys := make([]string, 0, len(filter))
	for k := range filter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		// 同样显式转型：->> 的右操作数必须是 text，比较值也一样。
		q = q.Where("(data ->> ?::text) = ?::text", k, filter[k])
	}
	return q
}
