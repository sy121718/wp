// Package productmodel 商品域持久化（issue #5 / T3a）。
//
// 本 model 是「表访问单元（Repository）」，只做本模块表的 CRUD 与聚合内原子组合；
// 业务规则（状态、slug 派生、默认值继承、唯一性预检）一律留在 service 层。
//
// gorm tag 不写 default 子句：默认值由 service 显式赋值，DDL 侧已有 DEFAULT。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ProductEntity 商品主体。价格与库存在变体上；关联关系走 JSON 列。
type ProductEntity struct {
	ID                  string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID           string          `gorm:"column:project_id;type:uuid;not null"`
	Name                string          `gorm:"column:name;type:text;not null"`
	Subtitle            string          `gorm:"column:subtitle;type:text;not null"`
	Description         json.RawMessage `gorm:"column:description;type:jsonb;not null"`
	Slug                string          `gorm:"column:slug;type:text;not null"`
	Status              string          `gorm:"column:status;type:text;not null"`
	Sort                int             `gorm:"column:sort;not null"`
	Unit                string          `gorm:"column:unit;type:text;not null"`
	Weight              *float64        `gorm:"column:weight;type:numeric(12,3)"`
	SEOTitle            string          `gorm:"column:seo_title;type:text;not null"`
	SEODescription      string          `gorm:"column:seo_description;type:text;not null"`
	Images              json.RawMessage `gorm:"column:images;type:jsonb;not null"`
	CategoryIDs         json.RawMessage `gorm:"column:category_ids;type:jsonb;not null"`
	TagIDs              json.RawMessage `gorm:"column:tag_ids;type:jsonb;not null"`
	RelatedIDs          json.RawMessage `gorm:"column:related_ids;type:jsonb;not null"`
	BundleItems         json.RawMessage `gorm:"column:bundle_items;type:jsonb;not null"`
	BrandID             *string         `gorm:"column:brand_id;type:uuid"`
	DefaultPrice        *float64        `gorm:"column:default_price;type:numeric(12,2)"`
	DefaultComparePrice *float64        `gorm:"column:default_compare_price;type:numeric(12,2)"`
	DefaultCostPrice    *float64        `gorm:"column:default_cost_price;type:numeric(12,2)"`
	DefaultImage        string          `gorm:"column:default_image;type:text;not null"`
	Metadata            json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt           time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt           time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 实现 gorm 表名。
func (ProductEntity) TableName() string { return "products" }

// VariantEntity 商品变体（一行一个 SKU）。
//
// StockTotal 是仓库模块库存真源的冗余缓存，仅用于列表展示；一切可用量判断
// 必须走仓库模块的契约读真源，绝不读这个字段（spec §库存 死线）。
type VariantEntity struct {
	ID            string          `gorm:"column:id;type:uuid;primaryKey"`
	ProductID     string          `gorm:"column:product_id;type:uuid;not null"`
	SKUCode       string          `gorm:"column:sku_code;type:text;not null"`
	Barcode       string          `gorm:"column:barcode;type:text;not null"`
	Price         float64         `gorm:"column:price;type:numeric(12,2);not null"`
	ComparePrice  *float64        `gorm:"column:compare_price;type:numeric(12,2)"`
	CostPrice     *float64        `gorm:"column:cost_price;type:numeric(12,2)"`
	Image         string          `gorm:"column:image;type:text;not null"`
	OptionValues  json.RawMessage `gorm:"column:option_values;type:jsonb;not null"`
	Enabled       bool            `gorm:"column:enabled;not null"`
	Sort          int             `gorm:"column:sort;not null"`
	StockTotal    int             `gorm:"column:stock_total;not null"`
	StockSyncedAt *time.Time      `gorm:"column:stock_synced_at"`
	Metadata      json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt     time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt     time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 实现 gorm 表名。
func (VariantEntity) TableName() string { return "product_variants" }

// Model 商品域仓储。
type Model struct{ db *gorm.DB }

// NewModel 构造（不持有业务状态）。
func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

// DB 本模块表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductEntity{})
}

// VariantDB 变体表句柄（同上，仅本 model 内部使用）。
func (m *Model) VariantDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VariantEntity{})
}

// CreateWithVariants 在同一事务内写商品行与其初始变体（聚合内原子组合）。
func (m *Model) CreateWithVariants(ctx context.Context, e *ProductEntity, variants []*VariantEntity) (err error) {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		for _, v := range variants {
			if err := tx.Create(v).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Get 按 ID 查商品。
func (m *Model) Get(ctx context.Context, id string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// SlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) SlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	q := m.DB(ctx).Where("project_id = ? AND slug = ?", projectID, slug)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// List 商品列表。只取列表需要的列：metadata 与 description 不参与列表查询（spec：默认不取）。
func (m *Model) List(ctx context.Context, projectID, keyword, status string, limit, offset int) (list []*ProductEntity, err error) {
	q := m.DB(ctx).Select("id, project_id, name, slug, status, images, sort, default_image, created_at, updated_at")
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		q = q.Where("name ILIKE ?", "%"+keyword+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err = q.Order("sort ASC, created_at DESC").Limit(limit).Offset(offset).Find(&list).Error
	return list, err
}

// Count 列表总数（与 List 同过滤条件）。
func (m *Model) Count(ctx context.Context, projectID, keyword, status string) (n int64, err error) {
	q := m.DB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		q = q.Where("name ILIKE ?", "%"+keyword+"%")
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err = q.Count(&n).Error
	return n, err
}

// Update 更新商品行（全字段保存）。
func (m *Model) Update(ctx context.Context, e *ProductEntity) (err error) {
	return m.DB(ctx).Where("id = ?", e.ID).Save(e).Error
}

// Delete 删除商品（变体由外键 ON DELETE CASCADE 连带删除）。
func (m *Model) Delete(ctx context.Context, id string) (err error) {
	return m.DB(ctx).Where("id = ?", id).Delete(&ProductEntity{}).Error
}

// ListVariants 某商品全部变体。
func (m *Model) ListVariants(ctx context.Context, productID string) (list []*VariantEntity, err error) {
	err = m.VariantDB(ctx).Where("product_id = ?", productID).Order("sort ASC, created_at ASC").Find(&list).Error
	return list, err
}

// ListVariantsByProducts 批量取多个商品的变体（列表页算价格区间，避免 N+1）。
func (m *Model) ListVariantsByProducts(ctx context.Context, productIDs []string) (list []*VariantEntity, err error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	err = m.VariantDB(ctx).Where("product_id IN ?", productIDs).Order("sort ASC").Find(&list).Error
	return list, err
}

// GetVariant 按 ID 查变体。
func (m *Model) GetVariant(ctx context.Context, id string) (e *VariantEntity, err error) {
	e = &VariantEntity{}
	err = m.VariantDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// SKUExists 同商品下 SKU 编码是否被占用。
func (m *Model) SKUExists(ctx context.Context, productID, skuCode, excludeID string) (exists bool, err error) {
	q := m.VariantDB(ctx).Where("product_id = ? AND sku_code = ?", productID, skuCode)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateVariant 写入变体。
func (m *Model) CreateVariant(ctx context.Context, e *VariantEntity) (err error) {
	return m.VariantDB(ctx).Create(e).Error
}

// UpdateVariant 更新变体。
func (m *Model) UpdateVariant(ctx context.Context, e *VariantEntity) (err error) {
	return m.VariantDB(ctx).Where("id = ?", e.ID).Save(e).Error
}

// DeleteVariant 删除变体。
func (m *Model) DeleteVariant(ctx context.Context, id string) (err error) {
	return m.VariantDB(ctx).Where("id = ?", id).Delete(&VariantEntity{}).Error
}
