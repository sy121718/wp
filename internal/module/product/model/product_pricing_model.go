// product_pricing_model.go — 定价工具的表访问与留痕读写（issue #13）。
//
// 本文件是「表访问单元」：只做 product_price_adjustments / product_price_adjustment_items
// 的读写，以及筛选集取数（products 单表条件查询）。
// 规则校验、算价、尾数处理、作用范围解析一律在 service（product_pricing_rule.go / product_pricing.go）。
//
// 留痕写入与价格写入必须原子：批次头 + 明细是同一聚合内的原子组合（CreateAdjustmentWithItemsTx），
// 与价格写入的跨聚合事务由 service 用 Transaction 编排（见 product_pricing.go 的 ApplyPricing）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// PriceAdjustmentEntity 调价批次（一次「按规则应用」一行）。
type PriceAdjustmentEntity struct {
	ID string `gorm:"column:id;type:uuid;primaryKey"`
	// ProjectID 作用范围所在工程（决策与后台列表的隔离维度）。
	ProjectID string `gorm:"column:project_id;type:uuid;not null"`
	// RuleType 内置定价规则类型（cost_multiple / cost_markup / target_margin / fixed_price）。
	RuleType string `gorm:"column:rule_type;type:text;not null"`
	// RuleParams 归一后的规则参数（键集合由规则类型决定）。
	RuleParams json.RawMessage `gorm:"column:rule_params;type:jsonb;not null"`
	// Rounding 尾数处理方式（none / integer / end_9 / end_99）。
	Rounding string `gorm:"column:rounding;type:text;not null"`
	// Scope 作用范围（sku / product / filter）。
	Scope string `gorm:"column:scope;type:text;not null"`
	// TargetID 单个 SKU / 单个商品范围的目标 id（筛选集范围为 NULL）。
	TargetID *string `gorm:"column:target_id;type:uuid"`
	// Filter 筛选集范围条件（status / keyword / categoryId / brandId / tagId）。
	Filter json.RawMessage `gorm:"column:filter;type:jsonb;not null"`
	// VariantCount 本次参与试算的变体数；ChangedCount 实际改动的变体数。
	VariantCount int    `gorm:"column:variant_count;not null"`
	ChangedCount int    `gorm:"column:changed_count;not null"`
	Note         string `gorm:"column:note;type:text;not null"`
	// OperatorID 操作人 id（取自会话；脚本 / 测试路径为空串）。
	OperatorID string    `gorm:"column:operator_id;type:text;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (PriceAdjustmentEntity) TableName() string { return "product_price_adjustments" }

// PriceAdjustmentItemEntity 调价批次逐变体明细（原价 → 新价）。
//
// 不建指向 product_variants 的外键（与 081 的关联列精简原则一致）：
// 变体后续被删掉，历史留痕仍要可读，故 SKU 编码与商品 id 都存当时的快照。
type PriceAdjustmentItemEntity struct {
	ID           string    `gorm:"column:id;type:uuid;primaryKey"`
	AdjustmentID string    `gorm:"column:adjustment_id;type:uuid;not null"`
	ProductID    string    `gorm:"column:product_id;type:uuid;not null"`
	VariantID    string    `gorm:"column:variant_id;type:uuid;not null"`
	SKUCode      string    `gorm:"column:sku_code;type:text;not null"`
	OldPrice     float64   `gorm:"column:old_price;type:numeric(12,2);not null"`
	NewPrice     float64   `gorm:"column:new_price;type:numeric(12,2);not null"`
	CreatedAt    time.Time `gorm:"column:create_time;not null"`
}

// TableName 实现 gorm 表名。
func (PriceAdjustmentItemEntity) TableName() string { return "product_price_adjustment_items" }

// PricingFilter 定价工具筛选集的作用条件（只读 products 单表；工程归属由 service 校验）。
type PricingFilter struct {
	ProjectID  string
	Status     string
	Keyword    string
	CategoryID string
	BrandID    string
	TagID      string
}

// AdjustmentDB 调价批次表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) AdjustmentDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PriceAdjustmentEntity{})
}

// AdjustmentItemDB 调价明细表句柄（同上）。
func (m *Model) AdjustmentItemDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PriceAdjustmentItemEntity{})
}

// CreateAdjustmentWithItemsTx 在给定事务内写批次头与其明细（聚合内原子组合）。
//
// 只写「有改动」的变体明细：没改动的行留痕没有信息量，还会把台账撑大。
func (m *Model) CreateAdjustmentWithItemsTx(tx *gorm.DB, e *PriceAdjustmentEntity, items []*PriceAdjustmentItemEntity) (err error) {
	// product_price_adjustments 有策略，scope 设在调用方事务上（另开事务会脱离外层原子性）。
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	if err = tx.Create(e).Error; err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	return tx.CreateInBatches(items, 200).Error
}

// GetAdjustment 按 ID 查批次。
//
// projectID 由**调用方**给出（不是从行里读回来的）：product_price_adjustments 在
// 迁移 215 名单里，跨工程的批次在策略下不可见，读不到即 ErrRecordNotFound。
func (m *Model) GetAdjustment(ctx context.Context, id, projectID string) (e *PriceAdjustmentEntity, err error) {
	e = &PriceAdjustmentEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&PriceAdjustmentEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// ListAdjustments 工程内的调价批次（新的在前）；limit <= 0 表示不限条数。
//
// projectID 必填（空串会被 rls 以 ErrInvalidProjectID 拒掉）：留痕列表是**工程内**
// 的视图，此前 projectID 为空即退化成「所有工程的批次」——策略生效后那种调用
// 会静默返回 0 行，所以把「必须指明工程」提到入口断言。
func (m *Model) ListAdjustments(ctx context.Context, projectID string, limit int) (list []*PriceAdjustmentEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&PriceAdjustmentEntity{}).
			Where("project_id = ?", projectID).
			Order("create_time DESC, id DESC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// ListAdjustmentItems 某批次的逐变体明细（按写入顺序，稳定可读）。
//
// 明细表 product_price_adjustment_items **不带 project_id、也不在迁移 215 名单里**
// （它靠父批次头归属工程，见实体注释），所以这里没有策略可依据。仍然把它收进
// 工程作用域：调用方总是先按工程读到批次头再取明细，两者落在同一个隔离上下文里，
// 将来若给明细表补上策略也不会漏掉这条路径。
func (m *Model) ListAdjustmentItems(ctx context.Context, adjustmentID, projectID string, limit int) (list []*PriceAdjustmentItemEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&PriceAdjustmentItemEntity{}).
			Where("adjustment_id = ?", adjustmentID).
			Order("create_time ASC, sku_code ASC, id ASC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// ListProductsForPricing 筛选集取数：按条件取工程内商品（单表查询，条件以参数传入）。
//
// 只取筛选与展示需要的列；关键词只匹配商品名（与后台列表口径一致）。
// 分类 / 标签是 JSONB 数组，用包含谓词命中 GIN 索引（与 ListProductsByTag 同一手法）。
func (m *Model) ListProductsForPricing(ctx context.Context, f PricingFilter) (list []*ProductEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listProductsForPricing(tx, ctx, f, &list)
	})
	return list, err
}

// listProductsForPricing 在调用方给定的事务句柄上执行筛选集查询。
//
// 单独拆出来是因为 products 在 215 名单里：查询必须落在**已设作用域的那个事务**上，
// 用 model 的裸句柄（m.db）会另取连接、另起事务，作用域读不到。
func (m *Model) listProductsForPricing(tx *gorm.DB, ctx context.Context, f PricingFilter, out *[]*ProductEntity) error {
	q := tx.WithContext(ctx).Model(&ProductEntity{}).
		Select("id, project_id, name, slug, status, sort, category_ids, brand_id, tag_ids, create_time")
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		q = q.Where("name ILIKE ?", "%"+f.Keyword+"%")
	}
	if f.CategoryID != "" {
		probe, merr := json.Marshal([]string{f.CategoryID})
		if merr != nil {
			return merr
		}
		q = q.Where("category_ids @> ?::jsonb", string(probe))
	}
	if f.TagID != "" {
		probe, merr := json.Marshal([]string{f.TagID})
		if merr != nil {
			return merr
		}
		q = q.Where("tag_ids @> ?::jsonb", string(probe))
	}
	if f.BrandID != "" {
		q = q.Where("brand_id = ?", f.BrandID)
	}
	return q.Order("sort ASC, create_time ASC, id ASC").Find(out).Error
}
