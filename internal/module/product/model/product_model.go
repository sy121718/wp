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

	"go_wp/pkg/rls"
)

// 商品类型（迁移 238）。
const (
	// TypeVariant 常规变体商品：价格与库存在自己的变体上。
	TypeVariant = "variant"
	// TypeBundle 捆绑容器：只有一个对外价格（products.default_price），
	// 成员变体只作为选项/履约明细，价格不参与计价与展示。
	TypeBundle = "bundle"
)

// ProductEntity 商品主体。价格与库存在变体上；关联关系走 JSON 列。
type ProductEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;not null"`
	Name        string          `gorm:"column:name;not null"`
	Subtitle    string          `gorm:"column:subtitle;not null"`
	Description json.RawMessage `gorm:"column:description;type:jsonb;not null"`
	Slug        string          `gorm:"column:slug;not null"`
	Status      string          `gorm:"column:status;not null"`
	// Type 商品类型（迁移 238）：TypeVariant = 常规变体商品（主体卖自己的 SKU）；
	// TypeBundle = 捆绑容器（只有容器价，成员价不参与计价与展示，库存按成员 BOM 扣减）。
	Type string `gorm:"column:type;not null"`
	// SKUCode 容器主体 SKU（迁移 246）—— 商品身份编码，不是「某个变体的 SKU」：
	//   · 变体商品：从仓库选 = <仓短码大写>_<仓库里那条 SKU 原样>；
	//     自己创建 = 运营填的编码（选了仓则自动附加仓码前缀）。
	//   · 捆绑商品：运营自定义，恒以 _B 结尾。
	// 变体商品的变体 SKU = 本值 + 属性值段… + _V（拼接在 service）。
	// 唯一性：同工程内唯一，由 uq_products_project_sku_code 部分唯一索引（sku_code <> ''）
	// 保证；**不要求全局唯一**（同一段编码可出现在不同仓库 / 不同工程）。
	// 存量商品为空串（新编码规则只作用于新建商品与新生成的变体，存量一律不重写）。
	SKUCode string `gorm:"column:sku_code;not null"`
	// PublishedAt 上架时间（issue #11）：最近一次进入 published 的时刻，由 service 在
	// 状态转 published 时写入；自动标签的「新品」规则以它为判定基准（不用 create_time，
	// 否则「建了草稿很久才上架」的商品会被误判成新品）。
	PublishedAt    *time.Time      `gorm:"column:published_at"`
	Sort           int             `gorm:"column:sort;not null"`
	Unit           string          `gorm:"column:unit;not null"`
	Weight         *float64        `gorm:"column:weight"`
	SEOTitle       string          `gorm:"column:seo_title;not null"`
	SEODescription string          `gorm:"column:seo_description;not null"`
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
	PrimaryCategoryID *string         `gorm:"column:primary_category_id"`
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
	BrandID             *string         `gorm:"column:brand_id"`
	DefaultPrice        *float64        `gorm:"column:default_price"`
	DefaultComparePrice *float64        `gorm:"column:default_compare_price"`
	DefaultCostPrice    *float64        `gorm:"column:default_cost_price"`
	DefaultImage        string          `gorm:"column:default_image;not null"`
	Metadata            json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt           time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt           time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductEntity) TableName() string { return "products" }

// VariantEntity 商品变体（一行一个 SKU）。
//
// 这里**没有**库存字段（issue #32）：库存真源在 inventory_stocks，商品侧的展示值
// 由查询期投影得到（同模块的库存用例提供真源汇总）。曾经的 stock_total 缓存列
// 与随之而来的同步 / 台账 / 对账已一并删除。
type VariantEntity struct {
	ID           string          `gorm:"column:id;primaryKey"`
	ProductID    string          `gorm:"column:product_id;not null"`
	SKUCode      string          `gorm:"column:sku_code;not null"`
	Barcode      string          `gorm:"column:barcode;not null"`
	Price        float64         `gorm:"column:price;not null"`
	ComparePrice *float64        `gorm:"column:compare_price"`
	CostPrice    *float64        `gorm:"column:cost_price"`
	Image        string          `gorm:"column:image;not null"`
	OptionValues json.RawMessage `gorm:"column:option_values;type:jsonb;not null"`
	Enabled      bool            `gorm:"column:enabled;not null"`
	Sort         int             `gorm:"column:sort;not null"`
	Metadata     json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt    time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt    time.Time       `gorm:"column:update_time;not null"`
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
	// RLS（迁移 215）：products 已启用 FORCE 策略，写入承 e.ProjectID 的工程作用域。
	// 变体表（product_variants）不在 215 的覆盖清单内，但同一事务里不受影响。
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.CreateWithVariantsTx(ctx, tx, e, variants)
	})
}

// CreateWithVariantsTx 与 CreateWithVariants 相同，但复用**调用方已开启的事务**。
//
// 事务边界由 service 决定（CQ-026）：一次商品保存要同时落「商品 + 首个变体 + 各仓库存行
// + 变更记录」，任一步失败必须整体回滚 —— 半截状态（有商品没有库存行、有变体没有留痕）
// 只能靠人工对账发现。tx 必须已由调用方设好工程作用域（rls.ScopeTx / InProjectScope），
// model 不再另开事务：另开会**另取一条连接**，外层未提交的数据在新连接里看不见，
// 原子性被悄悄破坏。
func (m *Model) CreateWithVariantsTx(ctx context.Context, tx *gorm.DB, e *ProductEntity, variants []*VariantEntity) (err error) {
	if err = tx.WithContext(ctx).Create(e).Error; err != nil {
		return err
	}
	for _, v := range variants {
		if v == nil {
			continue
		}
		if err = tx.WithContext(ctx).Create(v).Error; err != nil {
			return err
		}
	}
	return nil
}

// Get 按 ID 查商品。
//
// projectID 是**必填**的工程作用域：products 在迁移 215 名单里，策略谓词读的是会话变量
// （app.project_id），而 WHERE project_id = ? 只是普通过滤 —— 换连接角色后**没有作用域
// 的查询会静默返回 0 行**（fail closed 不报错）。空串会被 rls 直接拒掉，不会退化成
// 「查一个不存在的工程」那种更难排查的形态。
func (m *Model) Get(ctx context.Context, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetWithoutScope 按 ID 读商品行，**不设工程作用域**（审计 DB-009 的显式例外）。
//
// 原唯一调用方 ProductTranslationCandidates 已改成带作用域读（契约补了 projectID 形参，
// 翻译工作台把手里的当前工程传下来）。// 不设工程作用域，**当前没有任何生产调用方**（DB-009 第四批已把调用方改到带作用域的入口）。
//
// 它记录的是「拿不到工程上下文时的那一类入口」的形状：不加空串兜底（那会被 rls 拒掉，
// 把「静默 0 行」换成一个更难懂的错误），也不假装已被隔离。在非超级角色下它 fail closed
// （策略谓词为 NULL ⇒ 0 行 ⇒ ErrRecordNotFound）—— public/test/rls 的
// TestRLS_ProductTaxonomyScope_ExplicitExceptionsUnaffected 把这一形状钉住。
//
// 不要给本方法加新的调用方：需要按 id 读的一律用带 projectID 的那个。

func (m *Model) GetWithoutScope(ctx context.Context, id string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// ListByIDs 批量按 ID 取商品行，**必带工程作用域**。
//
// projectID 是**必填**的工程作用域（不是可选过滤条件）：products 在迁移 215 名单里，
// 策略谓词读会话变量 app.project_id，而 WHERE id IN (...) 只是普通过滤 —— 换连接角色后
// **没有作用域的查询会静默返回 0 行**（fail closed 不报错）。空串或非 uuid 会被 rls
// 直接拒掉（rls.ErrInvalidProjectID），不会退化成「查一个不存在的工程」那种更难排查的形态。
//
// 唯一调用方是 ProductService.VariantSnapshots（跨模块只读端口 VariantSnapshotPort），
// 消费方三条链：order 下单落快照 / cart 加购与结算 / productLivePrice 价格核对片段。
// 三者都能拿到工程（片段那条由商品组件把工程烘进片段 URL），所以这条链上不再保留
// 不带作用域的入口 —— 曾经这里是 ListByIDsWithoutScope，换非超级角色的后果是
// 「订单快照为空、加购拿不到变体、价格核对对不出任何结论」且一条错误日志都没有。
func (m *Model) ListByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// 闭包里用 tx，**绝不能退回 m.DB(ctx)**：那会另取一条连接、脱离事务，
	// 策略谓词读到的仍是 NULL，查询静默返回 0 行（与 collectionQuery 同一条禁令）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// SlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) SlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	// 唯一性判定要作用域：缺 scope 时恒「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND slug = ?", projectID, slug)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if cerr := q.Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	})
	return exists, err
}

// SKUCodeExists 主体 SKU（products.sku_code）在本工程内是否已被**其它商品**占用。
//
// 唯一性范围是**工程**（迁移 246 的偏唯一索引 uq_products_project_sku_code，sku_code <> ”),
// 与变体 SKU 的「同商品内唯一」（UNIQUE (product_id, sku_code)）是两条不同的约束 ——
// 对应的两个业务错误也不是同一件事（见 enums 的 ErrContainerSKUTaken / ErrSkuTaken）。
// excludeID 是编辑自己时排除自身（新建传空串）。
//
// 必须带工程作用域：products 有 FORCE 策略，缺作用域时计数恒 0 ⇒ 「不存在」⇒
// 重复创建被静默放行，随后在落库时撞唯一索引 —— 那正是要把原始 SQL 错误挡在页面外的原因。
// 空编码直接返回 false：偏索引排除空串，存量商品的主体编码为空是合法状态。
func (m *Model) SKUCodeExists(ctx context.Context, projectID, skuCode, excludeID string) (exists bool, err error) {
	code := strings.TrimSpace(skuCode)
	if code == "" {
		return false, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND sku_code = ?", projectID, code)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if cerr := q.Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	})
	return exists, err
}

// List 商品列表。只取列表需要的列：metadata 与 description 不参与列表查询（spec：默认不取）。
func (m *Model) List(ctx context.Context, projectID, keyword, status string, limit, offset int) (list []*ProductEntity, err error) {
	// 工程作用域必填：原本「projectID 为空即不限工程」在策略下会退化成读 0 行
	// （fail closed 不报错），是比裸查更难排查的静默失效。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductEntity{}).
			// sku_code 必须在 Select 里：列表少取它时，工具返回的「SKU=」永远是空串 ——
			// 而工具说明承诺了会给出 SKU（见 product_tools.go 的 product_find）。
			Select("id, project_id, name, slug, sku_code, type, status, images, sort, default_image, create_time, update_time")
		q = productKeywordFilter(q, keyword)
		if status != "" {
			q = q.Where("status = ?", status)
		}
		return q.Order("sort ASC, create_time DESC").Limit(limit).Offset(offset).Find(&list).Error
	})
	return list, err
}

// CollectionFilter 集合源的取数条件（issue #21：状态 + 分类 / 品牌 / 标签）。
//
// 全部是**等值**维度且彼此 AND —— 集合源只接受声明过的维度，不接受过滤表达式
// （不变量 4）。空串表示该维度不参与过滤。
//
// ProjectID 例外：它是**必填的工程作用域**（DB-009），不是可空过滤维度。products 在
// 迁移 215 名单里，ListForCollection / CountForCollection 拿它开 rls.InProjectScope ——
// 空串或非 uuid 直接返回 rls.ErrInvalidProjectID。不接受「不限工程」这种读法：换非超级
// 角色后它不会报错，只会静默退化成 0 行（集合卡整块变空，产物看着完全正常）。
type CollectionFilter struct {
	ProjectID  string
	Status     string
	CategoryID string
	BrandID    string
	TagID      string
	// CategoryIDs / BrandIDs 多值维度（与 TagIDs 同一形状、同一套语义）：
	// 逗号分隔的一组 id，CategoryAll / BrandAll 决定匹配语义。
	//
	// 与单值维度并存是刻意的：单值是既有配置的兼容面，多值是新配置的主路径。
	// 两者同时非空时**多值优先**（见 parseCollectionFilter）—— 单值只是「只勾了一个」
	// 的退化写法，让它们叠加成 AND 会让「我改成了多选」看起来毫无效果。
	CategoryIDs []string
	CategoryAll bool
	BrandIDs    []string
	BrandAll    bool
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

	// Options 属性值维度（issue #25）：属性组 key → 属性值 key 列表，逐项 AND。
	//
	// 值不在商品行上（attribute_ids 只存组引用），而在变体的 option_values JSONB 里，
	// 所以每项下推一条 EXISTS：「存在启用变体在该属性上取该值」。
	//
	// 一个属性组可以带多个值（多选筛选取并集）：组内 OR（存在变体取其中任一值）、
	// 组间 AND（每个属性组都要命中）—— 与多值标签的 `any` 语义同源，
	// 因为二者回答的是同一个问题：「这个商品的规格落在你勾的这堆值里吗」。
	Options map[string][]string
}

// sortedOptionKeys 属性维度键排序（谓词顺序确定，便于比对与排查）。
func sortedOptionKeys(options map[string][]string) []string {
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

// whereJsonbAnyOrAll 给查询加上 JSON 数组列的「任一命中 / 全部命中」条件（多值筛选的统一形状）。
//
// OR 展开成一串 `col @> jsonb_build_array(?::text)` 用 OR 连接，而不是 `?|` ——
// jsonb_path_ops 的 GIN 索引只支持 @>，换成 ?| 会退化成顺序扫描（081 建的索引白建）。
// 值来自解析期已校验的 uuid 列表，仍然逐个参数化，不拼进 SQL 字符串。
//
// **直接返回加好条件的 *gorm.DB，而不是 (sql, args) 二元组**：调用方写
// `q.Where(sql, args)` 时 args 是 []any，gorm 的变参会把整个切片当成**一个**标量实参
// 塞进第一个占位符，SQL 里于是留下 `?::text` 只被替换掉一个、其余原样（实测报
// `syntax error at or near "::"（SQLSTATE 42601）`）。让 helper 自己消费自己的参数，
// 这个坑从形状上就不存在了。
func whereJsonbAnyOrAll(q *gorm.DB, column string, ids []string, all bool) *gorm.DB {
	join := " OR "
	if all {
		join = " AND "
	}
	conds := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		conds = append(conds, column+" @> jsonb_build_array(?::text)")
		args = append(args, id)
	}
	return q.Where("("+strings.Join(conds, join)+")", args...)
}

// collectionQuery 集合源的查询构建：投影 + 全部过滤条件，List 与 Count 共用。
//
// 抽出来是为一件事：**分页的总量**（审计 PERF-019）必须按与取数完全一致的过滤条件去数。
// 过滤条件有十来个维度（工程 / 状态 / 分类 / 标签 / 价格区间 / 评分 / 在售 / 属性值…），
// 各自抄一份迟早会漂 —— 表现是「总页数按旧条件算」，一路翻到最后一页才发现少了几条，
// 而两条 SQL 分开看都是对的。
//
// 投影列必须覆盖集合项白名单里的全部字段来源：related（分类 / 品牌 / 标签）、
// tags、imageAlt / imageAlts（images_alt）都从这些列派生 —— 漏取任意一列，
// 对应的集合项字段就会**恒为空**（issue #22 排查商品卡标签时发现的 #9 遗留缺陷）。
// tx 由调用方给（ListForCollection / CountForCollection 的 InProjectScope 闭包）：
// 作用域就是事务级的 set_config，**绝不能在闭包里退回 m.DB(ctx)** —— 那会另取一条连接、
// 脱离事务，策略谓词读到的仍是 NULL，查询静默返回 0 行。
func (m *Model) collectionQuery(tx *gorm.DB, ctx context.Context, f CollectionFilter) *gorm.DB {
	// 投影列必须覆盖集合项白名单里的全部字段来源：related（分类 / 品牌 / 标签）、
	// tags、imageAlt / imageAlts（images_alt）都从这些列派生 —— 漏取任意一列，
	// 对应的集合项字段就会**恒为空**（issue #22 排查商品卡标签时发现的 #9 遗留缺陷：
	// 当时只取了列表展示需要的几列，白名单字段却已经放开了）。
	q := tx.WithContext(ctx).Model(&ProductEntity{}).Select(
		"id, project_id, name, subtitle, description, slug, status, sort, unit, " +
			"images, images_alt, attribute_ids, category_ids, tag_ids, brand_id, related_ids, " +
			"default_image, create_time, update_time, " +
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
	// 品牌：单值等值**列**（一个商品只属于一个品牌），多值走 IN。
	// 多值优先：两者同时非空时只看多值 —— 见 CollectionFilter 的注释。
	//
	// all 语义在这一维上几乎恒为空，但必须**显式**表达出来而不是退化成 IN：
	// 一个商品只有一个 brand_id，要求它同时等于两个不同的品牌是自相矛盾的 ——
	// 那是「交集为空」这个**结论**，不是「没有这个维度」。退化成 IN 的话，
	// 用户把语义切到 all 之后看到的却是并集（勾两个品牌反而出来更多商品），
	// 与分类 / 标签的 all 行为正好相反，是最容易被当成「筛选坏了」的一种。
	//
	// 写成 brand_id = A AND brand_id = B 让 SQL 自己去得到空集，而不是在这里
	// 提前 return：后者会在链式调用中间短路，把后面还没加的维度（价格 / 属性…）
	// 一起跳过 —— 那种「看情况跳过滤条件」比空结果难查得多。
	if len(f.BrandIDs) > 1 && f.BrandAll {
		for _, id := range f.BrandIDs {
			q = q.Where("brand_id = ?", id)
		}
	} else if len(f.BrandIDs) > 0 {
		q = q.Where("brand_id IN ?", f.BrandIDs)
	} else if f.BrandID != "" {
		q = q.Where("brand_id = ?", f.BrandID)
	}
	// 分类 / 标签是 JSON 数组列：用 @> 包含判断（走已有 GIN 索引 idx_products_*_ids），
	// 值经 jsonb_build_array 构造，完全参数化（不拼 SQL 字符串）。
	//
	// 多值（issue #27 起的统一样板）：OR 用「多个 @> 以 OR 连接」（每一项都能走 081 的
	// GIN 索引，planner 会用 BitmapOr 合并）；AND 就是逐条 @> 叠加。
	// 不以 ?| 实现 OR —— 那个操作符不吃 jsonb_path_ops 索引。
	if len(f.CategoryIDs) > 0 {
		q = whereJsonbAnyOrAll(q, "category_ids", f.CategoryIDs, f.CategoryAll)
	} else if f.CategoryID != "" {
		q = q.Where("category_ids @> jsonb_build_array(?::text)", f.CategoryID)
	}
	if len(f.TagIDs) > 0 {
		q = whereJsonbAnyOrAll(q, "tag_ids", f.TagIDs, f.TagAll)
	} else if f.TagID != "" {
		q = q.Where("tag_ids @> jsonb_build_array(?::text)", f.TagID)
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
		rated := tx.WithContext(ctx).Model(&ProductRatingEntity{}).
			Select("product_id").Group("product_id").Having("AVG(score) >= ?", *f.MinRating)
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
	// 一个属性组的多个值 = 组内 OR（存在变体取其中任一值），组间仍是 AND：
	// 多选筛选问的是「你的规格落在我勾的这堆值里吗」，不是「同时等于两个值」。
	for _, key := range sortedOptionKeys(f.Options) {
		values := f.Options[key]
		conds := make([]string, 0, len(values))
		args := make([]any, 0, len(values)*2)
		for _, value := range values {
			conds = append(conds,
				"EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = products.id"+
					" AND v.enabled AND v.option_values @> jsonb_build_object(?::text, ?::text))")
			args = append(args, key, value)
		}
		q = q.Where("("+strings.Join(conds, " OR ")+")", args...)
	}
	return q
}

// ListForCollection 集合源取数（issue #9）：一次取回集合项所需的全部白名单字段列。
//
// 与 List 的差异是刻意的：List 是后台列表（只要标题/图/状态那几列、按 update_time 语义），
// 集合源要的是「详情可绑定字段」的投影，且必须同一份确定性排序 ——
// 同一批数据每次构建输出同样字节（不变量 5）。
//
// offset 由调用方给（审计 PERF-019）：构建期恒为 0（只取第一屏），片段期按页码算。
// limit <= 0 表示由调用方那边的默认上限兜底。
func (m *Model) ListForCollection(ctx context.Context, f CollectionFilter, limit, offset int) (list []*ProductEntity, err error) {
	// 工程作用域取自 f.ProjectID（必填，见 CollectionFilter 的注释）：build 期与片段期
	// 都从 core.BuildProjectID(ctx) 拿 —— 缺工程时**显式报 ErrInvalidProjectID**，
	// 不退化成「不限工程」（后者换非超级角色后静默 0 行，集合卡整块变空）。
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := m.collectionQuery(tx, ctx, f).Order("sort ASC, create_time ASC, id ASC")
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// CountForCollection 满足同一组过滤条件的总条数（审计 PERF-019）：分页算总页数要用。
//
// 与 ListForCollection 共用 collectionQuery，两条查询不会各自漂移。
func (m *Model) CountForCollection(ctx context.Context, f CollectionFilter) (n int64, err error) {
	// 与 ListForCollection 同一把作用域与同一条 collectionQuery —— 取数与计数不会漂。
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.collectionQuery(tx, ctx, f).Count(&n).Error
	})
	return n, err
}

// productKeywordFilter 商品列表的关键词过滤（List 与 Count 共用同一份）。
//
// 两个字段一起搜：商品名（用户说「那个保温杯」）与 SKU 编码（用户说
// 「SKU 是 W1_A123 那个」）。只搜 name 时后一类问题**查不到任何结果**，
// 而工具说明又承诺了能按 SKU 找 —— 模型会以为自己查过了，然后编一个答案。
//
// 抽成函数而不是在两处各写一遍：List 与 Count 的过滤条件一旦分叉，
// 分页的总数徽章会与实际能翻到的条数对不上（那种偏差看起来只是「数字不准」）。
func productKeywordFilter(q *gorm.DB, keyword string) *gorm.DB {
	if keyword == "" {
		return q
	}
	pattern := "%" + keyword + "%"
	return q.Where("name ILIKE ? OR sku_code ILIKE ?", pattern, pattern)
}

// Count 列表总数（与 List 同过滤条件）。
func (m *Model) Count(ctx context.Context, projectID, keyword, status string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := productKeywordFilter(tx.WithContext(ctx).Model(&ProductEntity{}), keyword)
		if status != "" {
			q = q.Where("status = ?", status)
		}
		return q.Count(&n).Error
	})
	return n, err
}

// Update 更新商品行（全字段保存）。
func (m *Model) Update(ctx context.Context, e *ProductEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.UpdateTx(ctx, tx, e)
	})
}

// UpdateTx 与 Update 相同，但复用调用方事务（tx 已设好工程作用域，CQ-026）。
func (m *Model) UpdateTx(ctx context.Context, tx *gorm.DB, e *ProductEntity) (err error) {
	return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id = ?", e.ID).Save(e).Error
}

// Delete 删除商品（变体由外键 ON DELETE CASCADE 连带删除）。
//
// projectID 由调用方给出：products 在迁移 215 名单里，缺作用域时 DELETE 会静默匹配
// 0 行（不报错、也不删）——「点了删除但商品还在」正是本批要消灭的 fail-silent。
func (m *Model) Delete(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.DeleteTx(ctx, tx, id)
	})
}

// DeleteTx 与 Delete 相同，但复用调用方事务（删除与留痕同一事务，CQ-026）。
func (m *Model) DeleteTx(ctx context.Context, tx *gorm.DB, id string) (err error) {
	return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id = ?", id).Delete(&ProductEntity{}).Error
}

// ListVariants 某商品全部变体。
func (m *Model) ListVariants(ctx context.Context, productID string) (list []*VariantEntity, err error) {
	err = m.VariantDB(ctx).Where("product_id = ?", productID).Order("sort ASC, create_time ASC").Find(&list).Error
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
//
// projectID 是**必填**的工程作用域，但要如实说明它对当下的作用边界：
// product_variants 自身没有 project_id 列，**也不在迁移 215 的策略名单里**（实测
// relrowsecurity=f、policies=0），所以这条作用域**当前不对本表构成过滤** ——
// 变体的跨工程可见性是由调用方解析归属商品兜底的（ListProductsByIDs(ctx, ids, projectID)
// 读的是 products，那是有策略的表；bundle 的三条路径就是这么做的）。
// 仍然包作用域的两个理由：① 与同批读方法保持同一形状，避免换连接角色后出现
// 「有的方法静默 0 行、有的不会」这种难以推理的差异；② 将来给 product_variants
// 加策略时，这里不会突然退化成静默 0 行（那正是审计 db-03 §2.5 描述的失效形态）。
// 空串或非 uuid 由 rls 直接拒（rls.ErrInvalidProjectID）。
//
// public/test/rls/nonsuperuser 里有一条断言钉住这个现状：作用域 A 下拿两个工程的
// 变体 id 会**都读得到**，真正的隔离发生在归属商品那一步。
func (m *Model) ListVariantsByIDs(ctx context.Context, ids []string, projectID string) (list []*VariantEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&VariantEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// 捆绑成员引用的反查已迁到 product_ref_scan.go 的 ProductRefsByBundleVariant：
// 那条守卫必须**跨工程**可发现（别的工程把本工程的变体列为捆绑成员时，只在本工程里查
// 会命中 0 行 ⇒ 删除放行 ⇒ 那些捆绑的成员清单永久悬空），原来的单工程布尔反查
// （VariantReferencedByBundleItems）已随之删除 —— 留着它只会让下一个调用方再踩一次。

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
	return m.CreateVariantTx(ctx, m.db, e)
}

// CreateVariantTx 写入变体（复用调用方事务；product_variants 无 RLS 策略，无需作用域）。
func (m *Model) CreateVariantTx(ctx context.Context, tx *gorm.DB, e *VariantEntity) (err error) {
	return tx.WithContext(ctx).Create(e).Error
}

// UpdateVariantTx 更新变体（复用调用方事务）。
func (m *Model) UpdateVariantTx(ctx context.Context, tx *gorm.DB, e *VariantEntity) (err error) {
	return tx.WithContext(ctx).Model(&VariantEntity{}).Where("id = ?", e.ID).Save(e).Error
}

// DeleteVariantTx 删除变体（复用调用方事务；库存行由外键级联删除）。
func (m *Model) DeleteVariantTx(ctx context.Context, tx *gorm.DB, id string) (err error) {
	return tx.WithContext(ctx).Model(&VariantEntity{}).Where("id = ?", id).Delete(&VariantEntity{}).Error
}

// UpdateVariant 更新变体。
func (m *Model) UpdateVariant(ctx context.Context, e *VariantEntity) (err error) {
	return m.UpdateVariantTx(ctx, m.db, e)
}

// DeleteVariant 删除变体。
func (m *Model) DeleteVariant(ctx context.Context, id string) (err error) {
	return m.DeleteVariantTx(ctx, m.db, id)
}

// UpdateVariantCost 写变体成本价（只动 cost_price 一列，issue #18 的入库单价回写）。
//
// 售价、划线价、库存缓存一律不碰：成本口径与售价口径是两条独立的账。
func (m *Model) UpdateVariantCost(ctx context.Context, variantID string, cost float64, at time.Time) (err error) {
	return m.UpdateVariantCostTx(ctx, nil, variantID, cost, at)
}

// UpdateVariantCostTx 与 UpdateVariantCost 相同，但复用调用方事务。
//
// 成本价回写与它的主数据变更记录必须同事务：分成两次提交时，留痕失败会留下
// 「成本价改了、时间线上没有这一笔」—— 而成本价正是定价工具的成本口径，错账只能靠对账发现。
// tx 为 nil 即退化为按 ctx 取句柄（与不带 Tx 后缀的同名方法等价）。
func (m *Model) UpdateVariantCostTx(ctx context.Context, tx *gorm.DB, variantID string, cost float64, at time.Time) (err error) {
	handle := tx
	if handle == nil {
		handle = m.db.WithContext(ctx)
	}
	res := handle.WithContext(ctx).Model(&VariantEntity{}).Where("id = ?", variantID).
		Updates(map[string]any{"cost_price": cost, "update_time": at})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
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
