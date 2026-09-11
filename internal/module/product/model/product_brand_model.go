// product_brand_model.go — 商品品牌表访问（issue #10）。
//
// 品牌是独立实体（081 已建）：logo、描述、状态位在表上，工程内 slug 唯一。
// 本文件是「表访问单元」：只做 product_brands 的 CRUD，删除前置校验在 service。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ProductBrandEntity 商品品牌。
type ProductBrandEntity struct {
	ID             string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID      string          `gorm:"column:project_id;type:uuid;not null"`
	Name           string          `gorm:"column:name;type:text;not null"`
	Slug           string          `gorm:"column:slug;type:text;not null"`
	Logo           string          `gorm:"column:logo;type:text;not null"`
	Description    string          `gorm:"column:description;type:text;not null"`
	SEOTitle       string          `gorm:"column:seo_title;type:text;not null"`
	SEODescription string          `gorm:"column:seo_description;type:text;not null"`
	Sort           int             `gorm:"column:sort;not null"`
	Metadata       json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"column:created_at;not null"`
	UpdatedAt      time.Time       `gorm:"column:updated_at;not null"`
}

// TableName 实现 gorm 表名。
func (ProductBrandEntity) TableName() string { return "product_brands" }

// BrandDB 品牌表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) BrandDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductBrandEntity{})
}

// GetBrand 按 ID 查品牌。
func (m *Model) GetBrand(ctx context.Context, id string) (e *ProductBrandEntity, err error) {
	e = &ProductBrandEntity{}
	err = m.BrandDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// BrandSlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) BrandSlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	q := m.BrandDB(ctx).Where("project_id = ? AND slug = ?", projectID, slug)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListBrands 工程内品牌列表（条件以参数传入，按排序号 + 创建时间稳定排序）。
func (m *Model) ListBrands(ctx context.Context, projectID, keyword string) (list []*ProductBrandEntity, err error) {
	q := m.BrandDB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		q = q.Where("name ILIKE ?", "%"+keyword+"%")
	}
	err = q.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error
	return list, err
}

// ListBrandsByIDs 按 id 批量取品牌（集合源/构建期一次取好，零 N+1；顺序由调用方定）。
func (m *Model) ListBrandsByIDs(ctx context.Context, ids []string) (list []*ProductBrandEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.BrandDB(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// CreateBrand 写入品牌。
func (m *Model) CreateBrand(ctx context.Context, e *ProductBrandEntity) (err error) {
	return m.BrandDB(ctx).Create(e).Error
}

// UpdateBrand 更新品牌（整行保存）。
func (m *Model) UpdateBrand(ctx context.Context, e *ProductBrandEntity) (err error) {
	return m.BrandDB(ctx).Where("id = ?", e.ID).Save(e).Error
}

// DeleteBrand 删除品牌。products.brand_id 有外键 ON DELETE SET NULL，
// 但「被商品引用即拒绝删除」是业务规则，判定在 service。
func (m *Model) DeleteBrand(ctx context.Context, id string) (err error) {
	return m.BrandDB(ctx).Where("id = ?", id).Delete(&ProductBrandEntity{}).Error
}

// ProductUsingBrand 反查挂了某品牌的商品（删除前引用检查，只取一行用于拦截提示）。
func (m *Model) ProductUsingBrand(ctx context.Context, brandID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = m.DB(ctx).Where("brand_id = ?", brandID).
		Order("sort ASC, created_at ASC").Limit(1).Take(e).Error
	return e, err
}
