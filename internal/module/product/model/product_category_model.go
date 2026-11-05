package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/pagination"
	"go_wp/pkg/rls"
)

type ProductCategoryEntity struct {
	ID             string          `gorm:"column:id;primaryKey"`
	ProjectID      string          `gorm:"column:project_id;not null"`
	ParentID       *string         `gorm:"column:parent_id"`
	Name           string          `gorm:"column:name;not null"`
	Slug           string          `gorm:"column:slug;not null"`
	Description    string          `gorm:"column:description;not null"`
	Image          string          `gorm:"column:image;not null"`
	SEOTitle       string          `gorm:"column:seo_title;not null"`
	SEODescription string          `gorm:"column:seo_description;not null"`
	Sort           int             `gorm:"column:sort;not null"`
	Metadata       json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt      time.Time       `gorm:"column:update_time;not null"`
}

func (ProductCategoryEntity) TableName() string { return "product_categories" }

func (m *Model) CategoryDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductCategoryEntity{})
}

func (m *Model) GetCategory(ctx context.Context, id, projectID string) (e *ProductCategoryEntity, err error) {
	e = &ProductCategoryEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) GetCategoryWithoutScope(ctx context.Context, id string) (e *ProductCategoryEntity, err error) {
	e = &ProductCategoryEntity{}
	err = m.CategoryDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) CategorySlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
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

func (m *Model) ListCategories(ctx context.Context, projectID, keyword string) (list []*ProductCategoryEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductCategoryEntity{})
		if keyword != "" {
			q = q.Where(`name ILIKE ? ESCAPE '\'`, literalKeywordPattern(keyword))
		}
		return q.Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) CountCategories(ctx context.Context, projectID, keyword string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductCategoryEntity{})
		if keyword != "" {
			q = q.Where(`name ILIKE ? ESCAPE '\'`, literalKeywordPattern(keyword))
		}
		return q.Count(&n).Error
	})
	return n, err
}

type CategoryPageRow struct {
	ProductCategoryEntity
	HasChildren bool `gorm:"column:has_children"`
	Matched     bool `gorm:"column:matched"`
}

const categoryRootFilter = `project_id = ? AND (parent_id IS NULL OR NOT EXISTS (
				SELECT 1 FROM product_categories p
				WHERE p.id = product_categories.parent_id AND p.project_id = ?
			))`

func (m *Model) ListCategoryRootsPage(ctx context.Context, projectID string, limit, offset int) (rows []*CategoryPageRow, total int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		countQ := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Where(categoryRootFilter, projectID, projectID)
		if e := countQ.Count(&total).Error; e != nil {
			return e
		}
		if limit > 0 && int64(offset) >= total {
			page := pagination.ClampPage(offset/limit+1, limit, total)
			offset = (page - 1) * limit
		}

		var roots []ProductCategoryEntity
		if e := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Where(categoryRootFilter, projectID, projectID).
			Order("sort, create_time, id").
			Limit(limit).Offset(offset).
			Find(&roots).Error; e != nil {
			return e
		}
		all, e := categoryTreeDescendants(ctx, tx, projectID, roots)
		if e != nil {
			return e
		}
		rows = categoryRowsOf(all, nil)
		return nil
	})
	return rows, total, err
}

const categoryTreeMaxDepth = 64

func categoryTreeDescendants(ctx context.Context, tx *gorm.DB, projectID string, roots []ProductCategoryEntity) ([]ProductCategoryEntity, error) {
	all := make([]ProductCategoryEntity, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	layer := make([]string, 0, len(roots))
	for _, r := range roots {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		all = append(all, r)
		layer = append(layer, r.ID)
	}
	for depth := 0; depth < categoryTreeMaxDepth && len(layer) > 0; depth++ {
		var children []ProductCategoryEntity
		if err := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Where("project_id = ? AND parent_id IN ?", projectID, layer).
			Order("sort, create_time, id").
			Find(&children).Error; err != nil {
			return nil, err
		}
		next := make([]string, 0, len(children))
		for _, c := range children {
			if _, ok := seen[c.ID]; ok {
				continue
			}
			seen[c.ID] = struct{}{}
			all = append(all, c)
			next = append(next, c.ID)
		}
		layer = next
	}
	return all, nil
}

func categoryRowsOf(all []ProductCategoryEntity, matched map[string]bool) []*CategoryPageRow {
	hasChild := make(map[string]struct{}, len(all))
	for _, c := range all {
		if c.ParentID != nil && *c.ParentID != "" {
			hasChild[*c.ParentID] = struct{}{}
		}
	}
	out := make([]*CategoryPageRow, 0, len(all))
	for i := range all {
		_, has := hasChild[all[i].ID]
		out = append(out, &CategoryPageRow{
			ProductCategoryEntity: all[i],
			HasChildren:           has,
			Matched:               matched != nil && matched[all[i].ID],
		})
	}
	return out
}

func (m *Model) ListCategorySearchForest(ctx context.Context, projectID, keyword string) (rows []*CategoryPageRow, matchTotal int64, err error) {
	pattern := literalKeywordPattern(keyword)
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		matchQ := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Where(`project_id = ? AND (name ILIKE ? ESCAPE '\' OR slug ILIKE ? ESCAPE '\')`, projectID, pattern, pattern)
		if e := matchQ.Count(&matchTotal).Error; e != nil {
			return e
		}

		var picked []ProductCategoryEntity
		if e := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Where(`project_id = ? AND (name ILIKE ? ESCAPE '\' OR slug ILIKE ? ESCAPE '\')`, projectID, pattern, pattern).
			Order("sort, create_time, id").
			Find(&picked).Error; e != nil {
			return e
		}
		matched := make(map[string]bool, len(picked))
		all := make([]ProductCategoryEntity, 0, len(picked))
		seen := make(map[string]struct{}, len(picked))
		for _, p := range picked {
			matched[p.ID] = true
			if _, ok := seen[p.ID]; ok {
				continue
			}
			seen[p.ID] = struct{}{}
			all = append(all, p)
		}

		layer := make([]string, 0, len(picked))
		for _, p := range picked {
			if p.ParentID != nil && *p.ParentID != "" {
				layer = append(layer, *p.ParentID)
			}
		}
		for steps := 0; steps < categoryTreeMaxDepth && len(layer) > 0; steps++ {
			uniq := make([]string, 0, len(layer))
			for _, id := range layer {
				if _, ok := seen[id]; ok {
					continue
				}
				uniq = append(uniq, id)
			}
			if len(uniq) == 0 {
				break
			}
			var parents []ProductCategoryEntity
			if e := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
				Where("project_id = ? AND id IN ?", projectID, uniq).
				Order("sort, create_time, id").
				Find(&parents).Error; e != nil {
				return e
			}
			layer = layer[:0]
			for _, p := range parents {
				if _, ok := seen[p.ID]; ok {
					continue
				}
				seen[p.ID] = struct{}{}
				all = append(all, p)
				if p.ParentID != nil && *p.ParentID != "" {
					layer = append(layer, *p.ParentID)
				}
			}
		}
		rows = categoryRowsOf(all, matched)
		return nil
	})
	return rows, matchTotal, err
}

func (m *Model) ListCategoriesByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductCategoryEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) CreateCategory(ctx context.Context, e *ProductCategoryEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductCategoryEntity{}).Create(e).Error
	})
}

func (m *Model) UpdateCategory(ctx context.Context, e *ProductCategoryEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductCategoryEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

func (m *Model) DeleteCategory(ctx context.Context, id string) (err error) {
	return m.CategoryDB(ctx).Where("id = ?", id).Delete(&ProductCategoryEntity{}).Error
}

func (m *Model) CreateCategoryTx(ctx context.Context, tx *gorm.DB, e *ProductCategoryEntity) error {
	return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Create(e).Error
}

func (m *Model) UpdateCategoryTx(ctx context.Context, tx *gorm.DB, e *ProductCategoryEntity) error {
	return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteCategoryTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id = ?", id).Delete(&ProductCategoryEntity{}).Error
}

func (m *Model) CountCategoryChildren(ctx context.Context, parentID, projectID string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("parent_id = ?", parentID).Count(&n).Error
	})
	return n, err
}

func (m *Model) ProductUsingCategory(ctx context.Context, categoryID, projectID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID).
			Order("sort ASC, create_time ASC, id ASC").Limit(1).Take(e).Error
	})
	return e, err
}
