package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type BatchRefHit struct {
	RefColumns []string

	ProjectIDs []string
}

func (h *BatchRefHit) Referenced() bool { return h != nil && len(h.ProjectIDs) > 0 }

func (h *BatchRefHit) addProject(projectID string) {
	if h == nil || projectID == "" {
		return
	}
	for _, got := range h.ProjectIDs {
		if got == projectID {
			return
		}
	}
	h.ProjectIDs = append(h.ProjectIDs, projectID)
}

func (h *BatchRefHit) Outside(projectID string) []string {
	if h == nil {
		return nil
	}
	out := make([]string, 0, len(h.ProjectIDs))
	for _, pid := range h.ProjectIDs {
		if pid != projectID {
			out = append(out, pid)
		}
	}
	return out
}

type refBatchRow struct {
	ID                string          `gorm:"column:id"`
	ProjectID         string          `gorm:"column:project_id"`
	SKUCode           string          `gorm:"column:sku_code"`
	BrandID           *string         `gorm:"column:brand_id"`
	PrimaryCategoryID *string         `gorm:"column:primary_category_id"`
	CategoryIDs       json.RawMessage `gorm:"column:category_ids"`
	AttributeIDs      json.RawMessage `gorm:"column:attribute_ids"`
	TagIDs            json.RawMessage `gorm:"column:tag_ids"`
	RelatedIDs        json.RawMessage `gorm:"column:related_ids"`
}

func newBatchRefHits(entityIDs []string, refColumns []string) map[string]*BatchRefHit {
	out := make(map[string]*BatchRefHit, len(entityIDs))
	for _, id := range entityIDs {
		out[id] = &BatchRefHit{RefColumns: refColumns}
	}
	return out
}

func matchCandidateIDs(raw json.RawMessage, set map[string]bool) []string {
	if len(raw) == 0 {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

func (m *Model) eachRefScope(ctx context.Context, projectIDs []string, fn func(tx *gorm.DB, projectID string) error) error {
	scopes := normalizeProjectIDs(projectIDs)
	if len(scopes) == 0 {
		return nil
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pid := range scopes {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
			if err := fn(tx, pid); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Model) ProductRefsByCategoryIDs(ctx context.Context, categoryIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(categoryIDs)
	hits := newBatchRefHits(ids, []string{"products.category_ids", "products.primary_category_id"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, category_ids, primary_category_id").
			Where("project_id = ? AND (jsonb_exists_any(category_ids, ?::text[]) OR primary_category_id IN ?)",
				pid, textArrayParam(ids), ids).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].CategoryIDs, set) {
				hits[id].addProject(pid)
			}
			if pc := rows[i].PrimaryCategoryID; pc != nil && set[*pc] {
				hits[*pc].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func (m *Model) ProductRefsByBrandIDs(ctx context.Context, brandIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(brandIDs)
	hits := newBatchRefHits(ids, []string{"products.brand_id"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, brand_id").
			Where("project_id = ? AND brand_id IN ?", pid, ids).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if b := rows[i].BrandID; b != nil && set[*b] {
				hits[*b].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func (m *Model) ProductRefsByAttributeIDs(ctx context.Context, attributeIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(attributeIDs)
	hits := newBatchRefHits(ids, []string{"products.attribute_ids"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, attribute_ids").
			Where("project_id = ? AND jsonb_exists_any(attribute_ids, ?::text[])", pid, textArrayParam(ids)).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].AttributeIDs, set) {
				hits[id].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func (m *Model) ProductRefsByRelatedIDs(ctx context.Context, productIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(productIDs)
	hits := newBatchRefHits(ids, []string{"products.related_ids"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, related_ids").
			Where("project_id = ? AND jsonb_exists_any(related_ids, ?::text[])", pid, textArrayParam(ids)).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].RelatedIDs, set) {
				hits[id].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func (m *Model) ListRelatedReferrersTx(ctx context.Context, tx *gorm.DB, projectID string, ids []string) ([]*ProductEntity, error) {
	rows := make([]*ProductEntity, 0)
	if len(ids) == 0 || projectID == "" {
		return rows, nil
	}
	err := tx.WithContext(ctx).Model(&ProductEntity{}).
		Select("id, project_id, name, related_ids").
		Where("project_id = ? AND jsonb_exists_any(related_ids, ?::text[])", projectID, textArrayParam(ids)).
		Order("id ASC").
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Find(&rows).Error
	return rows, err
}

func (m *Model) UpdateRelatedIDsTx(ctx context.Context, tx *gorm.DB, projectID, id string, related json.RawMessage) error {
	return tx.WithContext(ctx).Model(&ProductEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).
		Updates(map[string]any{"related_ids": related, "update_time": time.Now()}).Error
}

func (m *Model) ProductRefsByTagIDs(ctx context.Context, tagIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(tagIDs)
	hits := newBatchRefHits(ids, []string{"products.tag_ids"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, tag_ids").
			Where("project_id = ? AND jsonb_exists_any(tag_ids, ?::text[])", pid, textArrayParam(ids)).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].TagIDs, set) {
				hits[id].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}
