// product_tag_model.go — 商品标签表访问 + 「商品 ↔ 标签」归属读写（issue #11）。
//
// 标签与规则合一张表（081 已建）：kind=manual 的手工标签、kind=rule 的自动标签，
// 规则类型存 rule_type、参数存 rule_params（JSONB）。本文件是「表访问单元」：
// 只做 product_tags 的 CRUD、products.tag_ids 的读写、以及规则求值所需的取数；
// 内置规则类型与参数的合法性判定、重算时机一律在 service（product_tag_rule.go / product_tag.go）。
//
// 归属写在 products.tag_ids（JSONB 数组，081 建了 GIN 索引）：手工标签与自动标签共用同一个
// 数组列，靠「重算只替换自己那一个 tag id」保证两者互不覆盖（见 ReplaceTagProductsTx）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ProductTagEntity 商品标签（手工 / 自动规则同表）。
type ProductTagEntity struct {
	ID        string `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID string `gorm:"column:project_id;type:uuid;not null"`
	Name      string `gorm:"column:name;type:text;not null"`
	Slug      string `gorm:"column:slug;type:text;not null"`
	// Kind manual=手工挂载 / rule=按内置规则自动维护。
	Kind string `gorm:"column:kind;type:text;not null"`
	// RuleType 内置规则类型（kind=manual 时为空串，091 的 CHECK 约束维持这个形状）。
	RuleType string `gorm:"column:rule_type;type:text;not null"`
	// RuleParams 规则参数（JSONB 对象；键集合由规则类型决定）。
	RuleParams json.RawMessage `gorm:"column:rule_params;type:jsonb;not null"`
	// RecalcAt 最近一次按规则重算的时间（手工标签恒为 NULL）。
	RecalcAt  *time.Time      `gorm:"column:recalc_at"`
	Sort      int             `gorm:"column:sort;not null"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductTagEntity) TableName() string { return "product_tags" }

// TagDB 标签表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) TagDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductTagEntity{})
}

// Transaction 透传事务（service 编排跨聚合原子写入，如「解绑商品引用 + 删除标签」）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// GetTag 按 ID 查标签。
func (m *Model) GetTag(ctx context.Context, id string) (e *ProductTagEntity, err error) {
	e = &ProductTagEntity{}
	err = m.TagDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// TagSlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) TagSlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	q := m.TagDB(ctx).Where("project_id = ? AND slug = ?", projectID, slug)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListTags 工程内标签列表（条件以参数传入；按排序号 + 创建时间稳定排序）。
// kind 为空表示不过滤（后台列表要同时展示手工与自动标签）。
func (m *Model) ListTags(ctx context.Context, projectID, kind, keyword string) (list []*ProductTagEntity, err error) {
	q := m.TagDB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if keyword != "" {
		q = q.Where("name ILIKE ?", "%"+keyword+"%")
	}
	err = q.Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	return list, err
}

// ListTagsByIDs 批量取标签（商品引用校验用，避免 N+1）。
func (m *Model) ListTagsByIDs(ctx context.Context, ids []string) (list []*ProductTagEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.TagDB(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// CreateTag 写入标签。
func (m *Model) CreateTag(ctx context.Context, e *ProductTagEntity) (err error) {
	return m.TagDB(ctx).Create(e).Error
}

// UpdateTag 更新标签（整行保存）。
func (m *Model) UpdateTag(ctx context.Context, e *ProductTagEntity) (err error) {
	return m.TagDB(ctx).Where("id = ?", e.ID).Save(e).Error
}

// DeleteTagTx 在给定事务里删除标签行（service 编排：先解绑引用再删）。
func (m *Model) DeleteTagTx(tx *gorm.DB, id string) (err error) {
	return tx.Model(&ProductTagEntity{}).Where("id = ?", id).Delete(&ProductTagEntity{}).Error
}

// ListProductsByTag 反查挂了某标签的商品（后台「某标签命中哪些商品」）。
//
// tag_ids 是 JSONB 数组：用包含谓词命中 GIN 索引；limit <= 0 表示不限条数。
func (m *Model) ListProductsByTag(ctx context.Context, tagID string, limit int) (list []*ProductEntity, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	q := m.DB(ctx).Where("tag_ids @> ?::jsonb", string(probe)).
		Order("sort ASC, create_time ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err = q.Find(&list).Error
	return list, err
}

// CountProductsByTag 某标签命中的商品数（列表页只数不取行）。
func (m *Model) CountProductsByTag(ctx context.Context, tagID string) (n int64, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return 0, merr
	}
	err = m.DB(ctx).Where("tag_ids @> ?::jsonb", string(probe)).Count(&n).Error
	return n, err
}

// ReplaceTagProductsTx 把某标签的商品归属整体替换为 productIDs（service 编排的原子组合）。
//
// 两步必须在同一事务里：先摘掉本工程下所有带这个 tag id 的商品，再挂到新命中的商品上。
// 只动这一个 tag id（jsonb 数组的 - 与 || 都是按元素操作），因此**不会碰其它标签** ——
// 这正是「自动标签重算不覆盖手工标签归属」的实现基础。
func (m *Model) ReplaceTagProductsTx(tx *gorm.DB, tagID, projectID string, productIDs []string, now time.Time) (err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return merr
	}
	// 摘：本工程下带这个 tag id 的行全部去掉它。
	if err = tx.Exec(
		"UPDATE products SET tag_ids = tag_ids - ?::text, update_time = ? WHERE project_id = ? AND tag_ids @> ?::jsonb",
		tagID, now, projectID, string(probe)).Error; err != nil {
		return err
	}
	if len(productIDs) == 0 {
		return nil
	}
	// 挂：命中集合一次写出（id 列表走 jsonb 数组参数展开，避免动态拼 SQL）。
	ids, merr := json.Marshal(productIDs)
	if merr != nil {
		return merr
	}
	return tx.Exec(
		"UPDATE products SET tag_ids = tag_ids || ?::jsonb, update_time = ? "+
			"WHERE project_id = ? AND id IN (SELECT (jsonb_array_elements_text(?::jsonb))::uuid)",
		string(probe), now, projectID, string(ids)).Error
}

// RemoveTagFromProductsTx 在给定事务里摘掉本工程所有商品上的某标签（删除标签前调用）。
func (m *Model) RemoveTagFromProductsTx(tx *gorm.DB, tagID, projectID string, now time.Time) (err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return merr
	}
	q := "UPDATE products SET tag_ids = tag_ids - ?::text, update_time = ? WHERE tag_ids @> ?::jsonb"
	args := []any{tagID, now, string(probe)}
	if projectID != "" {
		q += " AND project_id = ?"
		args = append(args, projectID)
	}
	return tx.Exec(q, args...).Error
}

// ListProductsByIDs 批量取商品行（引用校验、工程过滤用，避免 N+1）。
func (m *Model) ListProductsByIDs(ctx context.Context, ids []string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// ListProductIDsPublishedSince 取工程内「上架时间在 since 之后」的已发布商品 id
// （新品规则的求值来源；单表查询，条件以参数传入）。
func (m *Model) ListProductIDsPublishedSince(ctx context.Context, projectID, status string, since time.Time) (ids []string, err error) {
	err = m.DB(ctx).
		Where("project_id = ? AND status = ? AND published_at IS NOT NULL AND published_at >= ?", projectID, status, since).
		Distinct().Pluck("id", &ids).Error
	return ids, err
}

// ListProductIDsByVariantPrice 取「存在启用变体且价格落在区间内」的商品 id。
//
// 变体表没有 project_id，故这里只按价格筛出候选（单表查询），工程归属由 service 兜底过滤。
// min/max 为 nil 表示该方向不限。
func (m *Model) ListProductIDsByVariantPrice(ctx context.Context, min, max *float64) (ids []string, err error) {
	q := m.VariantDB(ctx).Where("enabled = ?", true)
	if min != nil {
		q = q.Where("price >= ?", *min)
	}
	if max != nil {
		q = q.Where("price <= ?", *max)
	}
	err = q.Distinct().Pluck("product_id", &ids).Error
	return ids, err
}

// ListProductIDsWithDiscount 取「存在启用变体且对比价高于售价」的商品 id（促销规则求值来源）。
func (m *Model) ListProductIDsWithDiscount(ctx context.Context) (ids []string, err error) {
	err = m.VariantDB(ctx).
		Where("enabled = ? AND compare_price IS NOT NULL AND compare_price > price", true).
		Distinct().Pluck("product_id", &ids).Error
	return ids, err
}
