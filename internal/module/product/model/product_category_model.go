// product_category_model.go — 商品分类表访问（issue #10）。
//
// 分类是树形自引用表（081 已建）：父子层级、工程内 slug 唯一、排序与 SEO 字段都在表上。
// 本文件仍是「表访问单元」：只做 product_categories 的 CRUD，不判环、不派生 slug、
// 不做删除前置校验（那些在 service）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductCategoryEntity 商品分类（树形自引用；parent_id 为空即顶级）。
type ProductCategoryEntity struct {
	ID             string          `gorm:"column:id;type:uuid;primaryKey"`
	ProjectID      string          `gorm:"column:project_id;type:uuid;not null"`
	ParentID       *string         `gorm:"column:parent_id;type:uuid"`
	Name           string          `gorm:"column:name;type:text;not null"`
	Slug           string          `gorm:"column:slug;type:text;not null"`
	Description    string          `gorm:"column:description;type:text;not null"`
	Image          string          `gorm:"column:image;type:text;not null"`
	SEOTitle       string          `gorm:"column:seo_title;type:text;not null"`
	SEODescription string          `gorm:"column:seo_description;type:text;not null"`
	Sort           int             `gorm:"column:sort;not null"`
	Metadata       json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt      time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductCategoryEntity) TableName() string { return "product_categories" }

// CategoryDB 分类表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) CategoryDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductCategoryEntity{})
}

// GetCategory 按 ID 查分类。
func (m *Model) GetCategory(ctx context.Context, id string) (e *ProductCategoryEntity, err error) {
	e = &ProductCategoryEntity{}
	err = m.CategoryDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// CategorySlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) CategorySlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	q := m.CategoryDB(ctx).Where("project_id = ? AND slug = ?", projectID, slug)
	if excludeID != "" {
		q = q.Where("id <> ?", excludeID)
	}
	var n int64
	if err = q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListCategories 工程内分类列表（条件以参数传入；同级按排序号 + 创建时间稳定排序）。
//
// 返回的是**扁平**列表：树的组装（父子挂接与环数据兜底）在 service，model 只负责读。
func (m *Model) ListCategories(ctx context.Context, projectID, keyword string) (list []*ProductCategoryEntity, err error) {
	q := m.CategoryDB(ctx)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		q = q.Where("name ILIKE ?", "%"+keyword+"%")
	}
	err = q.Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	return list, err
}

// ListCategoriesByIDs 批量取分类（商品引用校验与反查用，避免 N+1）。
func (m *Model) ListCategoriesByIDs(ctx context.Context, ids []string) (list []*ProductCategoryEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = m.CategoryDB(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// CreateCategory 写入分类。
func (m *Model) CreateCategory(ctx context.Context, e *ProductCategoryEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductCategoryEntity{}).Create(e).Error
	})
}

// UpdateCategory 更新分类（整行保存；ParentID 为 nil 时写 NULL = 提升为顶级）。
func (m *Model) UpdateCategory(ctx context.Context, e *ProductCategoryEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductCategoryEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// DeleteCategory 删除分类。子级由外键 ON DELETE SET NULL 兜底提升为顶级，
// 但「有子级即拒绝删除」是业务规则，判定在 service。
func (m *Model) DeleteCategory(ctx context.Context, id string) (err error) {
	return m.CategoryDB(ctx).Where("id = ?", id).Delete(&ProductCategoryEntity{}).Error
}

// CountCategoryChildren 直接子级数量（删除前置校验）。
func (m *Model) CountCategoryChildren(ctx context.Context, parentID string) (n int64, err error) {
	err = m.CategoryDB(ctx).Where("parent_id = ?", parentID).Count(&n).Error
	return n, err
}

// ProductUsingCategory 反查挂了某分类的商品（删除前引用检查）。
//
// 两个引用面都查：附属分类走 category_ids 的 jsonb 包含谓词，主分类走真列。
// 只取一行用于拦截提示，故 Limit(1)；命中多条时取排序最靠前的一条。
func (m *Model) ProductUsingCategory(ctx context.Context, categoryID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = m.DB(ctx).
		Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID).
		Order("sort ASC, create_time ASC").Limit(1).Take(e).Error
	return e, err
}
