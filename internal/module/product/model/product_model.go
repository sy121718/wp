package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/database"
	"go_wp/pkg/rls"
)

type ProductEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;not null"`
	Name        string          `gorm:"column:name;not null"`
	Subtitle    string          `gorm:"column:subtitle;not null"`
	Description json.RawMessage `gorm:"column:description;type:jsonb;not null"`
	Slug        string          `gorm:"column:slug;not null"`
	Status      string          `gorm:"column:status;not null"`

	SKUCode string `gorm:"column:sku_code;not null"`

	PublishedAt    *time.Time      `gorm:"column:published_at"`
	Sort           int             `gorm:"column:sort;not null"`
	Unit           string          `gorm:"column:unit;not null"`
	Weight         *float64        `gorm:"column:weight"`
	SEOTitle       string          `gorm:"column:seo_title;not null"`
	SEODescription string          `gorm:"column:seo_description;not null"`
	Images         json.RawMessage `gorm:"column:images;type:jsonb;not null"`

	ImageAlts json.RawMessage `gorm:"column:images_alt;type:jsonb;not null"`

	AttributeIDs json.RawMessage `gorm:"column:attribute_ids;type:jsonb;not null"`
	CategoryIDs  json.RawMessage `gorm:"column:category_ids;type:jsonb;not null"`

	PrimaryCategoryID *string         `gorm:"column:primary_category_id"`
	TagIDs            json.RawMessage `gorm:"column:tag_ids;type:jsonb;not null"`

	Ratings []ProductRatingEntity `gorm:"foreignKey:ProductID;references:ID"`

	RatingAvg   *float64 `gorm:"column:rating_avg;->"`
	RatingCount *int     `gorm:"column:rating_count;->"`

	MinPrice   *float64        `gorm:"column:min_price;->"`
	RelatedIDs json.RawMessage `gorm:"column:related_ids;type:jsonb;not null"`
	BrandID    *string         `gorm:"column:brand_id"`

	DefaultPrice        *float64        `gorm:"column:default_price"`
	DefaultComparePrice *float64        `gorm:"column:default_compare_price"`
	DefaultCostPrice    *float64        `gorm:"column:default_cost_price"`
	DefaultImage        string          `gorm:"column:default_image;not null"`
	Metadata            json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt           time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt           time.Time       `gorm:"column:update_time;not null"`
}

func (ProductEntity) TableName() string { return "products" }

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

	WarehouseSKUs json.RawMessage `gorm:"column:warehouse_skus;type:jsonb;not null"`
	CreatedAt     time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt     time.Time       `gorm:"column:update_time;not null"`
}

func (VariantEntity) TableName() string { return "product_variants" }

func (e *VariantEntity) BeforeCreate(tx *gorm.DB) error {
	if len(e.WarehouseSKUs) == 0 {
		e.WarehouseSKUs = json.RawMessage("[]")
	}
	return nil
}

type Model struct{ db *gorm.DB }

func NewModel(db *gorm.DB) *Model { return &Model{db: db} }

func (m *Model) DB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductEntity{})
}

func (m *Model) VariantDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&VariantEntity{})
}

func (m *Model) RatingDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductRatingEntity{})
}

func (m *Model) CreateWithVariants(ctx context.Context, e *ProductEntity, variants []*VariantEntity) (err error) {

	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.CreateWithVariantsTx(ctx, tx, e, variants)
	})
}

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

func (m *Model) Get(ctx context.Context, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	})
	return e, err
}

func (m *Model) GetWithoutScope(ctx context.Context, id string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = m.DB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) ListByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

const productStatusFactColumns = "id, project_id, status"

const productNameFactColumns = "id, project_id, name"

const productPublicOptionColumns = "id, project_id, name, status, attribute_ids"

func (m *Model) ListStatusFactsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select(productStatusFactColumns).
			Where("project_id = ? AND id IN ?", projectID, ids).Find(&list).Error
	})
	return list, err
}

const productSummaryColumns = "id, project_id, name, slug"

func (m *Model) GetSummaryFacts(ctx context.Context, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select(productSummaryColumns).
			Where("project_id = ? AND id = ?", projectID, id).First(e).Error
	})
	return e, err
}

const productRefColumns = "id, project_id, status"

const productIDFactColumns = "id, project_id"

func (m *Model) ListIDFactsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select(productIDFactColumns).
			Where("project_id = ? AND id IN ?", projectID, ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) GetRefFacts(ctx context.Context, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select(productRefColumns).
			Where("project_id = ? AND id = ?", projectID, id).First(e).Error
	})
	return e, err
}

func (m *Model) GetPublicOptionFacts(ctx context.Context, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select(productPublicOptionColumns).
			Where("project_id = ? AND id = ?", projectID, id).First(e).Error
	})
	return e, err
}

func (m *Model) ListNameFactsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select(productNameFactColumns).
			Where("project_id = ? AND id IN ?", projectID, ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) SlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {

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

func (m *Model) List(ctx context.Context, projectID, keyword, status string, limit, offset int) (list []*ProductEntity, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, name, slug, sku_code, status, images, images_alt, attribute_ids, category_ids, primary_category_id, brand_id, tag_ids, sort, default_image, default_price, create_time, update_time")
		q = productKeywordFilter(q, keyword)
		if status != "" {
			q = q.Where("status = ?", status)
		}

		return q.Order("sort ASC, create_time DESC, id ASC").Limit(limit).Offset(offset).Find(&list).Error
	})
	return list, err
}

func (m *Model) ListForVariantOptions(ctx context.Context, projectID string) (list []*ProductEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, name").
			Order("sort ASC, create_time DESC, id ASC").
			Find(&list).Error
	})
	return list, err
}

const idBatchSize = 500

type CollectionFilter struct {
	IDs        []string
	Order      string
	Keyword    string
	ProjectID  string
	Status     string
	CategoryID string
	BrandID    string
	TagID      string

	CategoryIDs []string
	CategoryAll bool
	BrandIDs    []string
	BrandAll    bool

	TagIDs []string

	TagAll bool

	OnSale bool

	MinRating *float64

	MinPrice *float64
	MaxPrice *float64

	Options map[string][]string
}

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

const collectionSelectColumns = "id, project_id, name, subtitle, description, slug, status, sort, unit, " +
	"images, images_alt, attribute_ids, category_ids, tag_ids, brand_id, related_ids, " +
	"default_image, create_time, update_time, " +

	"(SELECT MIN(v.price) FROM product_variants v WHERE v.product_id = products.id AND v.enabled) AS min_price, " +

	"(SELECT AVG(r.score) FROM product_ratings r WHERE r.product_id = products.id) AS rating_avg, " +
	"(SELECT COUNT(*)::int FROM product_ratings r WHERE r.product_id = products.id) AS rating_count"

func (m *Model) collectionQuery(tx *gorm.DB, ctx context.Context, f CollectionFilter) *gorm.DB {

	q := tx.WithContext(ctx).Model(&ProductEntity{}).Select(collectionSelectColumns)
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.IDs != nil {

		q = q.Where("products.id = ANY((?::text[])::uuid[])", textArrayParam(f.IDs))
	}
	if f.Keyword != "" {
		pattern := literalKeywordPattern(f.Keyword)
		q = q.Where("(name ILIKE ? ESCAPE '\\' OR subtitle ILIKE ? ESCAPE '\\')", pattern, pattern)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}

	if len(f.BrandIDs) > 1 && f.BrandAll {
		for _, id := range f.BrandIDs {
			q = q.Where("brand_id = ?", id)
		}
	} else if len(f.BrandIDs) > 0 {
		q = q.Where("brand_id IN ?", f.BrandIDs)
	} else if f.BrandID != "" {
		q = q.Where("brand_id = ?", f.BrandID)
	}

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

	if f.MinRating != nil {

		rated := tx.WithContext(ctx).Model(&ProductRatingEntity{}).
			Select("product_id").Group("product_id").Having("AVG(score) >= ?", *f.MinRating)
		q = q.Where("products.id IN (?)", rated)
	}

	if f.OnSale {
		q = q.Where("EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = products.id" +
			" AND v.enabled AND v.compare_price IS NOT NULL AND v.compare_price > v.price)")
	}

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

func (m *Model) ListForCollection(ctx context.Context, f CollectionFilter, limit, offset int) (list []*ProductEntity, err error) {

	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := orderCollectionQuery(m.collectionQuery(tx, ctx, f), f.Order)
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) ListForCollectionPageWithTotal(ctx context.Context, f CollectionFilter, limit, offset int) (list []*ProductEntity, total int64, err error) {
	rows := make([]collectionPageRow, 0)
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := orderCollectionQuery(m.collectionQuery(tx, ctx, f), f.Order)
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}

		return q.Select(collectionSelectColumns + ", COUNT(*) OVER() AS total_count").Find(&rows).Error
	})
	if err != nil {
		return nil, 0, err
	}
	list = make([]*ProductEntity, 0, len(rows))
	for i := range rows {
		list = append(list, &rows[i].ProductEntity)
		if rows[i].TotalCount > total {
			total = rows[i].TotalCount
		}
	}
	return list, total, nil
}

type collectionPageRow struct {
	ProductEntity
	TotalCount int64 `gorm:"column:total_count"`
}

func orderCollectionQuery(q *gorm.DB, order string) *gorm.DB {
	switch order {
	case "newest":
		return q.Order("create_time DESC, id ASC")
	case "oldest":
		return q.Order("create_time ASC, id ASC")
	case "priceAsc":
		return q.Order("min_price ASC NULLS LAST, id ASC")
	case "priceDesc":
		return q.Order("min_price DESC NULLS LAST, id ASC")
	case "ratingDesc":
		return q.Order("rating_avg DESC NULLS LAST, id ASC")
	default:
		return q.Order("sort ASC, create_time ASC, id ASC")
	}
}

func (m *Model) CountForCollection(ctx context.Context, f CollectionFilter) (n int64, err error) {

	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.collectionQuery(tx, ctx, f).Count(&n).Error
	})
	return n, err
}

func productKeywordFilter(q *gorm.DB, keyword string) *gorm.DB {
	if keyword == "" {
		return q
	}
	pattern := literalKeywordPattern(keyword)
	return q.Where(`(name ILIKE ? ESCAPE '\' OR sku_code ILIKE ? ESCAPE '\')`, pattern, pattern)
}

// 商品模块唯一的 LIKE 关键词构造点，用户输入一律按字面量处理。
func literalKeywordPattern(keyword string) string {
	return "%" + database.EscapeLikePattern(keyword) + "%"
}

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

func (m *Model) Update(ctx context.Context, e *ProductEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.UpdateTx(ctx, tx, e)
	})
}

func (m *Model) UpdateTx(ctx context.Context, tx *gorm.DB, e *ProductEntity) (err error) {
	return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) Delete(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return m.DeleteTx(ctx, tx, id)
	})
}

func (m *Model) DeleteTx(ctx context.Context, tx *gorm.DB, id string) (err error) {
	return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id = ?", id).Delete(&ProductEntity{}).Error
}

func (m *Model) ListVariants(ctx context.Context, productID string) (list []*VariantEntity, err error) {
	err = m.VariantDB(ctx).Where("product_id = ?", productID).
		Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	return list, err
}

func (m *Model) ListVariantsByProducts(ctx context.Context, productIDs []string) (list []*VariantEntity, err error) {
	if len(productIDs) == 0 {
		return nil, nil
	}

	err = m.VariantDB(ctx).Where("product_id IN ?", productIDs).
		Order("sort ASC, id ASC").Find(&list).Error
	return list, err
}

const variantPriceFactColumns = "id, product_id, price, compare_price, enabled"

func (m *Model) ListVariantPriceFacts(ctx context.Context, productIDs []string) (list []*VariantEntity, err error) {
	if len(productIDs) == 0 {
		return nil, nil
	}

	err = m.VariantDB(ctx).Select(variantPriceFactColumns).
		Where("product_id IN ?", productIDs).Order("sort ASC, id ASC").Find(&list).Error
	return list, err
}

func (m *Model) ListVariantsByIDs(ctx context.Context, ids []string, projectID string) (list []*VariantEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&VariantEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

const variantPublicFactColumns = "id, product_id, price, enabled"

func (m *Model) ListVariantPublicFacts(ctx context.Context, ids []string, projectID string) (list []*VariantEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&VariantEntity{}).
			Select(variantPublicFactColumns).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

const variantOptionFactColumns = "id, product_id, sku_code, price, cost_price, enabled, sort"

func (m *Model) ListVariantOptionFacts(ctx context.Context, productIDs []string) (list []*VariantEntity, err error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	err = m.VariantDB(ctx).Select(variantOptionFactColumns).
		Where("product_id IN ?", productIDs).Order("sort ASC, id ASC").Find(&list).Error
	return list, err
}

const variantDisplayFactColumns = "id, product_id, sku_code, price, compare_price, image, option_values, enabled, sort"

func (m *Model) ListVariantDisplayFacts(ctx context.Context, productIDs []string) (list []*VariantEntity, err error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	err = m.VariantDB(ctx).Select(variantDisplayFactColumns).
		Where("product_id IN ?", productIDs).
		Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	return list, err
}

func (m *Model) GetVariantScoped(ctx context.Context, id, projectID string) (e *VariantEntity, err error) {
	e = &VariantEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&VariantEntity{}).
			Joins("JOIN products AS p ON p.id = product_variants.product_id").
			Where("product_variants.id = ? AND p.project_id = ?", id, projectID).
			First(e).Error
	})
	return e, err
}

func (m *Model) GetVariantScopedTx(ctx context.Context, tx *gorm.DB, id, projectID string) (e *VariantEntity, err error) {
	e = &VariantEntity{}
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&VariantEntity{}).
		Joins("JOIN products AS p ON p.id = product_variants.product_id").
		Where("product_variants.id = ? AND p.project_id = ?", id, projectID).
		First(e).Error
	return e, err
}

func (m *Model) GetVariant(ctx context.Context, id string) (e *VariantEntity, err error) {
	e = &VariantEntity{}
	err = m.VariantDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) GetVariantForUpdateTx(ctx context.Context, tx *gorm.DB, id, productID string) (e *VariantEntity, err error) {
	e = &VariantEntity{}
	err = tx.WithContext(ctx).Where("id = ? AND product_id = ?", id, productID).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(e).Error
	return e, err
}

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

func (m *Model) CreateVariant(ctx context.Context, e *VariantEntity) (err error) {
	return m.CreateVariantTx(ctx, m.db, e)
}

func (m *Model) CreateVariantTx(ctx context.Context, tx *gorm.DB, e *VariantEntity) (err error) {
	return tx.WithContext(ctx).Create(e).Error
}

func (m *Model) UpdateVariantTx(ctx context.Context, tx *gorm.DB, e *VariantEntity) (err error) {
	return tx.WithContext(ctx).Model(&VariantEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) UpdateVariantWarehouseSKUsTx(ctx context.Context, tx *gorm.DB, variantID string, refs json.RawMessage, at time.Time) (err error) {
	db := tx
	if db == nil {
		db = m.db
	}
	return db.WithContext(ctx).Model(&VariantEntity{}).
		Where("id = ?", variantID).
		Updates(map[string]any{"warehouse_skus": refs, "update_time": at}).Error
}

func (m *Model) DeleteVariantTx(ctx context.Context, tx *gorm.DB, id string) (err error) {
	return tx.WithContext(ctx).Model(&VariantEntity{}).Where("id = ?", id).Delete(&VariantEntity{}).Error
}

func (m *Model) UpdateVariant(ctx context.Context, e *VariantEntity) (err error) {
	return m.UpdateVariantTx(ctx, m.db, e)
}

func (m *Model) DeleteVariant(ctx context.Context, id string) (err error) {
	return m.DeleteVariantTx(ctx, m.db, id)
}

func (m *Model) UpdateVariantCost(ctx context.Context, variantID string, cost float64, at time.Time) (err error) {
	return m.UpdateVariantCostTx(ctx, nil, variantID, cost, at)
}

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

func (m *Model) SaveVariants(ctx context.Context, updated, created []*VariantEntity) (err error) {
	if len(updated) == 0 && len(created) == 0 {
		return nil
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return m.SaveVariantsTx(tx, updated, created)
	})
}

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

func (m *Model) GetForUpdateTx(ctx context.Context, tx *gorm.DB, id, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = tx.WithContext(ctx).Model(&ProductEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(e).Error
	return e, err
}

func (m *Model) ListAllColumnsForTranslation(ctx context.Context, projectID string) (list []*ProductEntity, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {

		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ?", projectID).
			Order("sort ASC, create_time DESC, id ASC").
			Find(&list).Error
	})
	return list, err
}
