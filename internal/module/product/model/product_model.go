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
	"sort"
	"strings"
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
	PrimaryCategoryID *string         `gorm:"column:primary_category_id;type:uuid"`
	TagIDs            json.RawMessage `gorm:"column:tag_ids;type:jsonb;not null"`
	// Ratings 评分明细（issue #30）：与商品是 hasMany 关联，详情页等路径仍可用 Preload。
	//
	// 商品表上**没有**评分列 —— 平均分与条数由明细算出（投影，不落库）；
	// ListForCollection 用子查询投影到 RatingAvg / RatingCount，避免 Preload 全量明细（PERF-004）。
	Ratings []ProductRatingEntity `gorm:"foreignKey:ProductID;references:ID"`
	// RatingAvg / RatingCount 仅查询投影列（非表字段）—— 必须带 `->` 只读标记：
	// 少了它 GORM 会把这两列写进 INSERT / UPDATE，而 products 表上根本没有这两列
	//（评分由 product_ratings 明细算出），于是每一次建商品都会以
	// `column "rating_avg" does not exist` 失败。
	RatingAvg   *float64 `gorm:"column:rating_avg;->"`
	RatingCount *int     `gorm:"column:rating_count;->"`

	// MinPrice 最低启用变体价（issue #28 的投影别名 min_price）。
	//
	// 只读：它是列表查询的派生列，不落库（写操作一律走变体自己的 price）。
	// 没有启用变体时为 NULL —— 组件按「无价」排到最后，不当成 0 元。
	MinPrice            *float64        `gorm:"column:min_price;->"`
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
// 这里**没有**库存字段（issue #32）：库存真源在 inventory_stocks，商品侧的展示值
// 由查询期投影得到（同模块的库存用例提供真源汇总）。曾经的 stock_total 缓存列
// 与随之而来的同步 / 台账 / 对账已一并删除。
type VariantEntity struct {
	ID           string          `gorm:"column:id;type:uuid;primaryKey"`
	ProductID    string          `gorm:"column:product_id;type:uuid;not null"`
	SKUCode      string          `gorm:"column:sku_code;type:text;not null"`
	Barcode      string          `gorm:"column:barcode;type:text;not null"`
	Price        float64         `gorm:"column:price;type:numeric(12,2);not null"`
	ComparePrice *float64        `gorm:"column:compare_price;type:numeric(12,2)"`
	CostPrice    *float64        `gorm:"column:cost_price;type:numeric(12,2)"`
	Image        string          `gorm:"column:image;type:text;not null"`
	OptionValues json.RawMessage `gorm:"column:option_values;type:jsonb;not null"`
	Enabled      bool            `gorm:"column:enabled;not null"`
	Sort         int             `gorm:"column:sort;not null"`
	Metadata     json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt    time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt    time.Time       `gorm:"column:updated_at;not null"`
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

// RatingDB 评分明细表句柄（issue #30）：只允许被本 model 的仓储方法消费。
func (m *Model) RatingDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductRatingEntity{})
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

// Get 按 ID 查商品。projectID 非空时追加工程归属条件（防跨工程 IDOR）。
func (m *Model) Get(ctx context.Context, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	q := m.DB(ctx).Where("id = ?", id)
	if strings.TrimSpace(projectID) != "" {
		q = q.Where("project_id = ?", projectID)
	}
	err = q.First(e).Error
	return e, err
}

// ListByIDs 批量按 ID 取商品（订单落快照时按变体反查商品名，避免 N+1）。
//
// 只返回命中的行：缺的那些就是「商品已被删除」，由 service 判断怎么处理。
func (m *Model) ListByIDs(ctx context.Context, ids []string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.DB(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
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
	// TagIDs 多标签维度（issue #27）：与单值 TagID 并存，TagAll 决定语义。
	TagIDs []string
	// TagAll 多标签匹配语义：true = 同时具备全部（AND）；false = 具备任一（OR，默认）。
	TagAll bool
	// OnSale 只看在售（存在启用变体「有划线价且划线价高于售价」，与 #11 的 on_sale 同源）。
	OnSale bool
	// MinRating 最低评分（issue #29）：只出 rating >= 该值的商品；无评分的不入选。
	MinRating *float64
	// MinPrice / MaxPrice 价格区间（issue #28）：筛「存在启用变体价格落在区间内」。
	// 各自可选：只有下限 = ≥ 下限，只有上限 = ≤ 上限，两个都有 = 闭区间。
	MinPrice *float64
	MaxPrice *float64

	// Options 属性值维度（issue #25）：属性组 key → 属性值 key，逐项 AND。
	//
	// 值不在商品行上（attribute_ids 只存组引用），而在变体的 option_values JSONB 里，
	// 所以每项下推一条 EXISTS：「存在启用变体在该属性上取该值」。
	Options map[string]string
}

// sortedOptionKeys 属性维度键排序（谓词顺序确定，便于比对与排查）。
func sortedOptionKeys(options map[string]string) []string {
	if len(options) == 0 {
		return nil
	}
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
			"default_image, created_at, updated_at, " +
			// 最低启用变体价（issue #28）：价格排序与价格区间展示都要数值，
			// 光有 priceRange 字符串没法排序。没有启用变体的商品该列为 NULL。
			"(SELECT MIN(v.price) FROM product_variants v WHERE v.product_id = products.id AND v.enabled) AS min_price, " +
			// 评分聚合（issue #30 / PERF-004）：一次子查询取均值与条数，不 Preload 全量明细。
			"(SELECT AVG(r.score) FROM product_ratings r WHERE r.product_id = products.id) AS rating_avg, " +
			"(SELECT COUNT(*)::int FROM product_ratings r WHERE r.product_id = products.id) AS rating_count")
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
	// 多标签（issue #27）：OR 用「多个 @> 以 OR 连接」（每一项都能走 081 的 GIN 索引，
	// planner 会用 BitmapOr 合并）；AND 就是逐条 @> 叠加。不以 ?| 实现 OR ——
	// 那个操作符不吃 jsonb_path_ops 索引。
	if len(f.TagIDs) > 0 {
		if f.TagAll {
			for _, id := range f.TagIDs {
				q = q.Where("tag_ids @> jsonb_build_array(?::text)", id)
			}
		} else {
			conds := make([]string, 0, len(f.TagIDs))
			args := make([]any, 0, len(f.TagIDs))
			for _, id := range f.TagIDs {
				conds = append(conds, "tag_ids @> jsonb_build_array(?::text)")
				args = append(args, id)
			}
			q = q.Where("("+strings.Join(conds, " OR ")+")", args...)
		}
	}
	// 价格区间（issue #28）：价格在变体上，与属性值同一条路 —— EXISTS 下推而不是列比较。
	// 只认**启用**变体：下架规格的价格不该把商品筛出来（与属性维度同一口径）。
	if f.MinPrice != nil || f.MaxPrice != nil {
		cond := "EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = products.id AND v.enabled"
		args := make([]any, 0, 2)
		if f.MinPrice != nil {
			cond += " AND v.price >= ?"
			args = append(args, *f.MinPrice)
		}
		if f.MaxPrice != nil {
			cond += " AND v.price <= ?"
			args = append(args, *f.MaxPrice)
		}
		q = q.Where(cond+")", args...)
	}
	// 最低评分（issue #29）：rating 为 NULL 的商品**直接排除**（不是当 0 分比较）——
	// 把「没有评分」当成 0 分会让新商品在「评分 ≥ 4」的筛选里永远消失，
	// 而它其实只是还没人评过。
	if f.MinRating != nil {
		// 最低评分（issue #30）：评分在独立表里，用**子查询**表达「有评分且均分 ≥ 阈值」——
		// 走 GORM 的模型句柄由它生成嵌套 SQL，而不是手写表名拼 JOIN。
		//
		// 为什么筛选必须在 SQL 侧：集合源一次最多取 100 条，先取回再在内存里筛是错的
		//（筛掉的可能本该排在前面）。排序则相反 —— 集合项已带投影出的 ratingValue，
		// 排在组件层做，不必进 SQL。
		rated := m.RatingDB(ctx).Select("product_id").Group("product_id").Having("AVG(score) >= ?", *f.MinRating)
		q = q.Where("products.id IN (?)", rated)
	}
	// 在售（issue #27）：与自动标签 on_sale 同一判定 —— 存在启用变体且划线价高于售价。
	if f.OnSale {
		q = q.Where("EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = products.id" +
			" AND v.enabled AND v.compare_price IS NOT NULL AND v.compare_price > v.price)")
	}
	// 属性值维度（issue #25）：值在变体上，逐个属性下推 EXISTS 子查询。
	//
	// 用 `option_values @> jsonb_build_object(key, value)` 而不是 `->> key = value`：
	// 前者能走迁移 118 的 GIN(jsonb_path_ops)（jsonb_path_ops 只支持 @>），后者只能用
	// 表达式索引 —— 两边的语义在这里等价（属性值 key 恒为字符串）。
	// 键按字典序遍历，谓词顺序确定（同输入同 SQL，产物可比对）。
	//
	// 只认**启用**变体：下架的规格组合不该把商品筛出来。
	for _, key := range sortedOptionKeys(f.Options) {
		q = q.Where(
			"EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = products.id"+
				" AND v.enabled AND v.option_values @> jsonb_build_object(?::text, ?::text))",
			key, f.Options[key])
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

// ListVariantIDs 取工程的变体 id 列表（issue #32：库存投影按变体 id 批量取真源汇总用）。
func (m *Model) ListVariantIDs(ctx context.Context, projectID string) (out []string, err error) {
	out = []string{}
	err = m.VariantDB(ctx).Where("project_id = ?", projectID).Order("id").Pluck("id", &out).Error
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
