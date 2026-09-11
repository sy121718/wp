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
	ID          string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;type:uuid;not null"`
	Name        string          `gorm:"column:name;type:text;not null"`
	Subtitle    string          `gorm:"column:subtitle;type:text;not null"`
	Description json.RawMessage `gorm:"column:description;type:jsonb;not null"`
	Slug        string          `gorm:"column:slug;type:text;not null"`
	Status      string          `gorm:"column:status;type:text;not null"`
	// PublishedAt 上架时间（issue #11）：最近一次进入 published 的时刻，由 service 在
	// 状态转 published 时写入；自动标签的「新品」规则以它为判定基准（不用 created_at，
	// 否则「建了草稿很久才上架」的商品会被误判成新品）。
	PublishedAt    *time.Time      `gorm:"column:published_at"`
	Sort           int             `gorm:"column:sort;not null"`
	Unit           string          `gorm:"column:unit;type:text;not null"`
	Weight         *float64        `gorm:"column:weight;type:numeric(12,3)"`
	SEOTitle       string          `gorm:"column:seo_title;type:text;not null"`
	SEODescription string          `gorm:"column:seo_description;type:text;not null"`
	Images         json.RawMessage `gorm:"column:images;type:jsonb;not null"`
	// ImageAlts 图集 alt 文本数组（issue #12，迁移 094）：与 Images 逐位对应，
	// 元素可为空串（该图仍是装饰性图片，产物由商品名兜底）。
	// alt 是作者文本，参与内容翻译（语境 product.imageAlts，逐元素取词）；URL 永不翻译。
	ImageAlts json.RawMessage `gorm:"column:images_alt;type:jsonb;not null"`
	// AttributeIDs 引用的属性组 id 数组（issue #7：同一属性组可被多个商品复用，
	// 商品侧只存引用，组与值的定义只存在一份）。
	AttributeIDs json.RawMessage `gorm:"column:attribute_ids;type:jsonb;not null"`
	CategoryIDs  json.RawMessage `gorm:"column:category_ids;type:jsonb;not null"`
	// PrimaryCategoryID 主分类（issue #10）：附属分类是 category_ids 数组，
	// 主分类需要「唯一 + 可反查 + 分类被删即自动解绑」，故落成真列 + 外键。
	// 不变量：主分类必然同时出现在 category_ids 里（由 service 维护）。
	PrimaryCategoryID   *string         `gorm:"column:primary_category_id;type:uuid"`
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

// CollectionFilter 集合源的取数条件（issue #21：状态 + 分类 / 品牌 / 标签）。
//
// 全部是**等值**维度且彼此 AND —— 集合源只接受声明过的维度，不接受过滤表达式
// （不变量 4）。空串表示该维度不参与过滤；ProjectID 为空表示不限工程。
type CollectionFilter struct {
	ProjectID  string
	Status     string
	CategoryID string
	BrandID    string
	TagID      string
}

// ListForCollection 集合源取数（issue #9）：一次取回集合项所需的全部白名单字段列。
//
// 与 List 的差异是刻意的：List 是后台列表（只要标题/图/状态那几列、按 updated_at 语义），
// 集合源要的是「详情可绑定字段」的投影（副标题/描述/单位/属性引用等），且必须同一份
// 确定性排序 —— 同一批数据每次构建输出同样字节（不变量 5）。
//
// 条件以参数传入（CollectionFilter 的各个等值维度 / 分页），方法内不写死业务判断；
// limit <= 0 表示不限条数。
func (m *Model) ListForCollection(ctx context.Context, f CollectionFilter, limit, offset int) (list []*ProductEntity, err error) {
	// 投影列必须覆盖集合项白名单里的全部字段来源：related（分类 / 品牌 / 标签）、
	// tags、imageAlt / imageAlts（images_alt）都从这些列派生 —— 漏取任意一列，
	// 对应的集合项字段就会**恒为空**（issue #22 排查商品卡标签时发现的 #9 遗留缺陷：
	// 当时只取了列表展示需要的几列，白名单字段却已经放开了）。
	q := m.DB(ctx).Select(
		"id, project_id, name, subtitle, description, slug, status, sort, unit, " +
			"images, images_alt, attribute_ids, category_ids, tag_ids, brand_id, related_ids, " +
			"default_image, created_at, updated_at")
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.BrandID != "" {
		q = q.Where("brand_id = ?", f.BrandID)
	}
	// 分类 / 标签是 JSON 数组列：用 @> 包含判断（走已有 GIN 索引 idx_products_*_ids），
	// 值经 jsonb_build_array 构造，完全参数化（不拼 SQL 字符串）。
	if f.CategoryID != "" {
		q = q.Where("category_ids @> jsonb_build_array(?::text)", f.CategoryID)
	}
	if f.TagID != "" {
		q = q.Where("tag_ids @> jsonb_build_array(?::text)", f.TagID)
	}
	q = q.Order("sort ASC, created_at ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	err = q.Find(&list).Error
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

// ListVariantsByIDs 批量按 ID 取变体（issue #20：捆绑选项按 id 批量取 SKU，避免 N+1）。
//
// 返回值只有命中的行：调用方按「请求了哪些 id」与「拿到了哪些」做差集，
// 缺的那些就是「SKU 已被删除」——那是业务判断，留在 service。
func (m *Model) ListVariantsByIDs(ctx context.Context, ids []string) (list []*VariantEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.VariantDB(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// VariantProjectIDs 变体 id → 所属工程 id（issue #24）。
//
// 变体表只存 product_id，没有工程列 —— 「按变体查可用量」的访问面调用方（片段端点）手里
// 只有变体 id，没有工程上下文，必须在这里反查补齐。两张表同属本模块，join 不越表隔离
// （跨模块才禁止）。
//
// 只返回命中的行：缺的那些就是「变体已被删除」，由 service 判断怎么处理。
func (m *Model) VariantProjectIDs(ctx context.Context, variantIDs []string) (out map[string]string, err error) {
	out = map[string]string{}
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows := []struct {
		ID        string `gorm:"column:id"`
		ProjectID string `gorm:"column:project_id"`
	}{}
	err = m.VariantDB(ctx).
		Table("product_variants AS v").
		Select("v.id AS id, p.project_id AS project_id").
		Joins("JOIN products AS p ON p.id = v.product_id").
		Where("v.id IN ?", variantIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.ProjectID
	}
	return out, nil
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

// CountVariants 某商品的变体数量（SKU 序号生成的前置计数）。
func (m *Model) CountVariants(ctx context.Context, productID string) (n int64, err error) {
	err = m.VariantDB(ctx).Where("product_id = ?", productID).Count(&n).Error
	return n, err
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

// SyncVariantStockTotal 写变体的库存缓存列（stock_total + stock_synced_at，issue #16）。
//
// 只更新这两列：价格、状态、SKU 等业务列一个字不动 —— 库存缓存同步不是「保存变体」，
// 用 Save(v) 全字段落库会在并发编辑下把作者刚改的价格冲掉。
// 变体不存在时返回 gorm.ErrRecordNotFound（PostgreSQL 的 UPDATE 即使值未变
// 也计入 RowsAffected，故 0 只可能是「没有这一行」）。
func (m *Model) SyncVariantStockTotal(ctx context.Context, variantID string, total int, at time.Time) (err error) {
	res := m.VariantDB(ctx).Where("id = ?", variantID).
		Updates(map[string]any{"stock_total": total, "stock_synced_at": at})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateVariantCost 写变体成本价（只动 cost_price 一列，issue #18 的入库单价回写）。
//
// 售价、划线价、库存缓存一律不碰：成本口径与售价口径是两条独立的账。
func (m *Model) UpdateVariantCost(ctx context.Context, variantID string, cost float64, at time.Time) (err error) {
	res := m.VariantDB(ctx).Where("id = ?", variantID).
		Updates(map[string]any{"cost_price": cost, "updated_at": at})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListVariantStockTotals 批量读变体的库存缓存值（variant id → stock_total，库存模块对账用）。
func (m *Model) ListVariantStockTotals(ctx context.Context, variantIDs []string) (out map[string]int, err error) {
	out = make(map[string]int, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID         string `gorm:"column:id"`
		StockTotal int    `gorm:"column:stock_total"`
	}
	if err = m.VariantDB(ctx).Select("id, stock_total").Where("id IN ?", variantIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.StockTotal
	}
	return out, nil
}

// SaveVariants 在同一事务内写一批变体改动：先更新既有行，再批量插入新行。
//
// 组合生成是「一次请求改多行」的聚合内原子组合（无规格占位变体就地承接第一个
// 组合 + 其余组合新建）：半截状态会把商品留在「一部分组合已生成」的中间态，
// 前台规格选择器随之少行，故必须原子。
func (m *Model) SaveVariants(ctx context.Context, updated, created []*VariantEntity) (err error) {
	if len(updated) == 0 && len(created) == 0 {
		return nil
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return m.SaveVariantsTx(tx, updated, created)
	})
}

// SaveVariantsTx 与 SaveVariants 相同，但复用调用方事务（service 编排跨聚合原子写入）。
//
// 定价工具（issue #13）一次应用要同时写「变体新价格」与「调价留痕」：
// 二者分别属变体聚合与留痕聚合，事务边界由 service 决定，model 只提供 tx 透传。
func (m *Model) SaveVariantsTx(tx *gorm.DB, updated, created []*VariantEntity) (err error) {
	for _, v := range updated {
		if err = tx.Model(&VariantEntity{}).Where("id = ?", v.ID).Save(v).Error; err != nil {
			return err
		}
	}
	if len(created) == 0 {
		return nil
	}
	return tx.CreateInBatches(created, 100).Error
}
