package productmodel

import (
	"cmp"
	"context"
	json "go_wp/pkg/jsonx"
	"slices"
	"sort"
	"strings"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

const maxRefSamplePerProject = 3

type RefProduct struct {
	ProductID string
	ProjectID string

	SKUCode string
}

type CrossProjectRef struct {
	RefColumns []string

	ProjectIDs []string

	ProductCounts map[string]int

	Products []RefProduct

	Total int
}

func (r *CrossProjectRef) Referenced() bool { return r != nil && r.Total > 0 }

func (r *CrossProjectRef) Outside(projectID string) *CrossProjectRef {
	projectID = strings.TrimSpace(projectID)
	if r == nil || projectID == "" {
		return r
	}
	out := &CrossProjectRef{
		RefColumns:    append([]string(nil), r.RefColumns...),
		ProductCounts: make(map[string]int, len(r.ProductCounts)),
	}
	for _, pid := range r.ProjectIDs {
		if pid == projectID {
			continue
		}
		out.ProjectIDs = append(out.ProjectIDs, pid)
		out.ProductCounts[pid] = r.ProductCounts[pid]
		out.Total += r.ProductCounts[pid]
	}
	for _, p := range r.Products {
		if p.ProjectID == projectID {
			continue
		}
		out.Products = append(out.Products, p)
	}
	return out
}

type refScanFilter func(db *gorm.DB) *gorm.DB

type refRow struct {
	ID        string `gorm:"column:id"`
	ProjectID string `gorm:"column:project_id"`
	SKUCode   string `gorm:"column:sku_code"`
}

func (m *Model) scanProductRefs(ctx context.Context, refColumns []string, projectIDs []string, filter refScanFilter) (ref *CrossProjectRef, err error) {
	ref = &CrossProjectRef{
		RefColumns:    refColumns,
		ProductCounts: map[string]int{},
	}
	scopes := normalizeProjectIDs(projectIDs)
	if len(scopes) == 0 {
		return ref, nil
	}

	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pid := range scopes {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
			rows := make([]refRow, 0, maxRefSamplePerProject)
			q := filter(tx.WithContext(ctx).Model(&ProductEntity{}).Where("project_id = ?", pid))
			if ferr := q.Select("id, project_id, sku_code").
				Order("create_time ASC, id ASC").
				Limit(maxRefSamplePerProject).
				Find(&rows).Error; ferr != nil {
				return ferr
			}
			if len(rows) == 0 {
				continue
			}
			hits := len(rows)
			if hits >= maxRefSamplePerProject {

				var cnt int64
				cq := filter(tx.WithContext(ctx).Model(&ProductEntity{}).Where("project_id = ?", pid))
				if cerr := cq.Count(&cnt).Error; cerr != nil {
					return cerr
				}
				hits = int(cnt)
			}
			ref.ProductCounts[pid] = hits
			ref.Total += hits
			ref.Products = append(ref.Products, sampleProducts(rows)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(ref.Products, func(a, b RefProduct) int {
		return cmp.Or(cmp.Compare(a.ProjectID, b.ProjectID), cmp.Compare(a.ProductID, b.ProductID))
	})
	for _, pid := range scopes {
		if ref.ProductCounts[pid] > 0 {
			ref.ProjectIDs = append(ref.ProjectIDs, pid)
		}
	}
	return ref, nil
}

func sampleProducts(rows []refRow) []RefProduct {
	out := make([]RefProduct, 0, len(rows))
	for _, r := range rows {
		out = append(out, RefProduct{ProductID: r.ID, ProjectID: r.ProjectID, SKUCode: r.SKUCode})
	}
	return out
}

func normalizeProjectIDs(projectIDs []string) []string {
	if len(projectIDs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(projectIDs))
	out := make([]string, 0, len(projectIDs))
	for _, raw := range projectIDs {
		pid := strings.TrimSpace(raw)
		if pid == "" || seen[pid] {
			continue
		}
		seen[pid] = true
		out = append(out, pid)
	}
	sort.Strings(out)
	return out
}

func (m *Model) ProductRefsByCategory(ctx context.Context, categoryID string, projectIDs []string) (*CrossProjectRef, error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductRefs(ctx,
		[]string{"products.category_ids", "products.primary_category_id"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID)
		})
}

func (m *Model) ProductRefsByBrand(ctx context.Context, brandID string, projectIDs []string) (*CrossProjectRef, error) {
	return m.scanProductRefs(ctx, []string{"products.brand_id"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("brand_id = ?", brandID)
		})
}

func (m *Model) ProductRefsByAttribute(ctx context.Context, attributeID string, projectIDs []string) (*CrossProjectRef, error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductRefs(ctx, []string{"products.attribute_ids"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("attribute_ids @> ?::jsonb", string(probe))
		})
}

func (m *Model) ProductRefsByTag(ctx context.Context, tagID string, projectIDs []string) (*CrossProjectRef, error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductRefs(ctx, []string{"products.tag_ids"}, projectIDs,
		func(db *gorm.DB) *gorm.DB {
			return db.Where("tag_ids @> ?::jsonb", string(probe))
		})
}

func (m *Model) ProductIDsByCategoryTx(ctx context.Context, tx *gorm.DB, projectID, categoryID string, limit int) (ids []string, err error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID)
	})
}

func (m *Model) ProductIDsByBrandTx(ctx context.Context, tx *gorm.DB, projectID, brandID string, limit int) (ids []string, err error) {
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("brand_id = ?", brandID)
	})
}

func (m *Model) ProductIDsByAttributeTx(ctx context.Context, tx *gorm.DB, projectID, attributeID string, limit int) (ids []string, err error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("attribute_ids @> ?::jsonb", string(probe))
	})
}

func (m *Model) ProductIDsByTagTx(ctx context.Context, tx *gorm.DB, projectID, tagID string, limit int) (ids []string, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	return m.scanProductIDsTx(ctx, tx, projectID, limit, func(db *gorm.DB) *gorm.DB {
		return db.Where("tag_ids @> ?::jsonb", string(probe))
	})
}

func (m *Model) scanProductIDsTx(ctx context.Context, tx *gorm.DB, projectID string, limit int,
	filter refScanFilter) (ids []string, err error) {
	if tx == nil || strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}
	q := filter(tx.WithContext(ctx).Model(&ProductEntity{}).Where("project_id = ?", projectID))
	err = q.Select("id::text").Order("id ASC").Limit(limit).Find(&ids).Error
	return ids, err
}
