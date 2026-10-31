package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type ProductBrandEntity struct {
	ID             string          `gorm:"column:id;primaryKey"`
	ProjectID      string          `gorm:"column:project_id;not null"`
	Name           string          `gorm:"column:name;not null"`
	Slug           string          `gorm:"column:slug;not null"`
	Logo           string          `gorm:"column:logo;not null"`
	Description    string          `gorm:"column:description;not null"`
	SEOTitle       string          `gorm:"column:seo_title;not null"`
	SEODescription string          `gorm:"column:seo_description;not null"`
	Sort           int             `gorm:"column:sort;not null"`
	Metadata       json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt      time.Time       `gorm:"column:update_time;not null"`
}

func (ProductBrandEntity) TableName() string { return "product_brands" }

func (m *Model) BrandDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductBrandEntity{})
}

func (m *Model) GetBrand(ctx context.Context, id, projectID string) (e *ProductBrandEntity, err error) {
	e = &ProductBrandEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) GetBrandWithoutScope(ctx context.Context, id string) (e *ProductBrandEntity, err error) {
	e = &ProductBrandEntity{}
	err = m.BrandDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) BrandSlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductBrandEntity{}).
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

func (m *Model) ListBrands(ctx context.Context, projectID, keyword string, limit, offset int) (list []*ProductBrandEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductBrandEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ?", productKeywordPattern(keyword))
		}
		q = q.Order("sort ASC, create_time ASC, id ASC")
		if limit > 0 {

			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) CountBrands(ctx context.Context, projectID, keyword string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductBrandEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ?", productKeywordPattern(keyword))
		}
		return q.Count(&n).Error
	})
	return n, err
}

func (m *Model) ListBrandsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductBrandEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) CreateBrand(ctx context.Context, e *ProductBrandEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductBrandEntity{}).Create(e).Error
	})
}

func (m *Model) UpdateBrand(ctx context.Context, e *ProductBrandEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductBrandEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

func (m *Model) DeleteBrand(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", id).Delete(&ProductBrandEntity{}).Error
	})
}

func (m *Model) CreateBrandTx(ctx context.Context, tx *gorm.DB, e *ProductBrandEntity) error {
	return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Create(e).Error
}

func (m *Model) UpdateBrandTx(ctx context.Context, tx *gorm.DB, e *ProductBrandEntity) error {
	return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteBrandTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", id).Delete(&ProductBrandEntity{}).Error
}

func (m *Model) ProductUsingBrand(ctx context.Context, brandID, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("brand_id = ?", brandID).
			Order("sort ASC, create_time ASC, id ASC").Limit(1).Take(e).Error
	})
	return e, err
}
