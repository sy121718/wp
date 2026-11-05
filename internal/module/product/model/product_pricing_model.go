package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type PriceAdjustmentEntity struct {
	ID string `gorm:"column:id;primaryKey"`

	ProjectID string `gorm:"column:project_id;not null"`

	RuleType string `gorm:"column:rule_type;not null"`

	RuleParams json.RawMessage `gorm:"column:rule_params;type:jsonb;not null"`

	Rounding string `gorm:"column:rounding;not null"`

	Scope string `gorm:"column:scope;not null"`

	TargetID *string `gorm:"column:target_id"`

	Filter json.RawMessage `gorm:"column:filter;type:jsonb;not null"`

	VariantCount int    `gorm:"column:variant_count;not null"`
	ChangedCount int    `gorm:"column:changed_count;not null"`
	Note         string `gorm:"column:note;not null"`

	OperatorID string    `gorm:"column:operator_id;not null"`
	CreatedAt  time.Time `gorm:"column:create_time;not null"`
}

func (PriceAdjustmentEntity) TableName() string { return "product_price_adjustments" }

type PriceAdjustmentItemEntity struct {
	ID           string    `gorm:"column:id;primaryKey"`
	AdjustmentID string    `gorm:"column:adjustment_id;not null"`
	ProductID    string    `gorm:"column:product_id;not null"`
	VariantID    string    `gorm:"column:variant_id;not null"`
	SKUCode      string    `gorm:"column:sku_code;not null"`
	OldPrice     float64   `gorm:"column:old_price;not null"`
	NewPrice     float64   `gorm:"column:new_price;not null"`
	CreatedAt    time.Time `gorm:"column:create_time;not null"`
}

func (PriceAdjustmentItemEntity) TableName() string { return "product_price_adjustment_items" }

type PricingFilter struct {
	ProjectID  string
	Status     string
	Keyword    string
	CategoryID string
	BrandID    string
	TagID      string
}

func (m *Model) AdjustmentDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PriceAdjustmentEntity{})
}

func (m *Model) AdjustmentItemDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&PriceAdjustmentItemEntity{})
}

func (m *Model) CreateAdjustmentWithItemsTx(tx *gorm.DB, e *PriceAdjustmentEntity, items []*PriceAdjustmentItemEntity) (err error) {

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

func (m *Model) GetAdjustment(ctx context.Context, id, projectID string) (e *PriceAdjustmentEntity, err error) {
	e = &PriceAdjustmentEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&PriceAdjustmentEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

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

func (m *Model) ListAdjustmentItemsByAdjustmentIDs(ctx context.Context, adjustmentIDs []string, projectID string) (list []*PriceAdjustmentItemEntity, err error) {
	if len(adjustmentIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		inProject := tx.WithContext(ctx).Model(&PriceAdjustmentEntity{}).
			Select("id").Where("project_id = ?", projectID).Where("id IN ?", adjustmentIDs)
		return tx.WithContext(ctx).Model(&PriceAdjustmentItemEntity{}).
			Where("adjustment_id IN (?)", inProject).
			Order("adjustment_id ASC, create_time ASC, sku_code ASC, id ASC").
			Find(&list).Error
	})
	return list, err
}

func (m *Model) ListProductsForPricing(ctx context.Context, f PricingFilter) (list []*ProductEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.listProductsForPricing(tx, ctx, f, &list)
	})
	return list, err
}

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
		q = q.Where(`name ILIKE ? ESCAPE '\'`, literalKeywordPattern(f.Keyword))
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
