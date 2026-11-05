package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

type ProductTagEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Name      string `gorm:"column:name;not null"`
	Slug      string `gorm:"column:slug;not null"`

	Kind string `gorm:"column:kind;not null"`

	RuleType string `gorm:"column:rule_type;not null"`

	RuleParams json.RawMessage `gorm:"column:rule_params;type:jsonb;not null"`

	RecalcAt  *time.Time      `gorm:"column:recalc_at"`
	Sort      int             `gorm:"column:sort;not null"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

func (ProductTagEntity) TableName() string { return "product_tags" }

func (m *Model) TagDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductTagEntity{})
}

func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

func (m *Model) GetTag(ctx context.Context, id, projectID string) (e *ProductTagEntity, err error) {
	e = &ProductTagEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductTagEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	})
	return e, err
}

func (m *Model) GetTagWithoutScope(ctx context.Context, id string) (e *ProductTagEntity, err error) {
	e = &ProductTagEntity{}
	err = m.TagDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) TagSlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductTagEntity{}).
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

func (m *Model) ListTags(ctx context.Context, projectID, kind, keyword string, limit, offset int) (list []*ProductTagEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductTagEntity{})
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		if keyword != "" {
			q = q.Where(`name ILIKE ? ESCAPE '\'`, literalKeywordPattern(keyword))
		}
		q = q.Order("sort ASC, create_time ASC, id ASC")
		if limit > 0 {

			q = q.Limit(limit).Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) CountTags(ctx context.Context, projectID, kind, keyword string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductTagEntity{})
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		if keyword != "" {
			q = q.Where(`name ILIKE ? ESCAPE '\'`, literalKeywordPattern(keyword))
		}
		return q.Count(&n).Error
	})
	return n, err
}

func (m *Model) ListTagsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductTagEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductTagEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) CreateTag(ctx context.Context, e *ProductTagEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.CreateTagTx(ctx, tx, e)
	})
}

func (m *Model) CreateTagTx(ctx context.Context, tx *gorm.DB, e *ProductTagEntity) (err error) {
	return tx.WithContext(ctx).Model(&ProductTagEntity{}).Create(e).Error
}

func (m *Model) UpdateTag(ctx context.Context, e *ProductTagEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.UpdateTagTx(ctx, tx, e)
	})
}

func (m *Model) UpdateTagTx(ctx context.Context, tx *gorm.DB, e *ProductTagEntity) (err error) {
	return tx.WithContext(ctx).Model(&ProductTagEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteTagTx(tx *gorm.DB, id string) (err error) {
	res := tx.Model(&ProductTagEntity{}).Where("id = ?", id).Delete(&ProductTagEntity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (m *Model) ListProductsByTag(ctx context.Context, tagID, projectID string, limit int) (list []*ProductEntity, err error) {
	return m.ListProductsByTagPage(ctx, tagID, projectID, 0, limit)
}

func (m *Model) ListProductsByTagPage(ctx context.Context, tagID, projectID string, offset, limit int) (list []*ProductEntity, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND tag_ids @> ?::jsonb", projectID, string(probe)).
			Order("sort ASC, create_time ASC, id ASC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		if offset > 0 {
			q = q.Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

func (m *Model) CountProductsByTag(ctx context.Context, tagID, projectID string) (n int64, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return 0, merr
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND tag_ids @> ?::jsonb", projectID, string(probe)).Count(&n).Error
	})
	return n, err
}

type TagHitCount struct {
	TagID string `gorm:"column:tag_id"`
	Total int64  `gorm:"column:total"`
}

func (m *Model) CountProductsByTagIDs(ctx context.Context, projectID string, tagIDs []string) (out map[string]int64, err error) {
	out = make(map[string]int64, len(tagIDs))
	if projectID == "" || len(tagIDs) == 0 {
		return out, nil
	}
	probe, merr := json.Marshal(tagIDs)
	if merr != nil {
		return nil, merr
	}

	var rows []TagHitCount
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).
			Table("(SELECT (jsonb_array_elements_text(?::jsonb))::uuid AS tag_id) AS w", string(probe)).
			Select("w.tag_id::text AS tag_id, COUNT(p.id) AS total").
			Joins("LEFT JOIN products p ON p.project_id = ? AND p.tag_ids @> jsonb_build_array(w.tag_id::text)", projectID).
			Group("w.tag_id").
			Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TagID] = r.Total
	}
	return out, nil
}

func (m *Model) ReplaceTagProductsTx(tx *gorm.DB, tagID, projectID string, productIDs []string, now time.Time) (err error) {

	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return merr
	}

	if err = tx.Model(&ProductEntity{}).
		Where("project_id = ? AND tag_ids @> ?::jsonb", projectID, string(probe)).
		UpdateColumns(map[string]any{
			"tag_ids":     gorm.Expr("tag_ids - ?::text", tagID),
			"update_time": now,
		}).Error; err != nil {
		return err
	}
	if len(productIDs) == 0 {
		return nil
	}

	ids, merr := json.Marshal(productIDs)
	if merr != nil {
		return merr
	}
	return tx.Model(&ProductEntity{}).
		Where("project_id = ? AND id IN (SELECT (jsonb_array_elements_text(?::jsonb))::uuid)", projectID, string(ids)).
		UpdateColumns(map[string]any{
			"tag_ids":     gorm.Expr("tag_ids || ?::jsonb", string(probe)),
			"update_time": now,
		}).Error
}

func (m *Model) ListProductIDsByTagTx(tx *gorm.DB, tagID, projectID string) (ids []string, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	var rows []struct {
		ID string `gorm:"column:id"`
	}
	if err = tx.Model(&ProductEntity{}).
		Select("id::text AS id").
		Where("project_id = ? AND tag_ids @> ?::jsonb", projectID, string(probe)).
		Order("id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	ids = make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func (m *Model) RemoveTagFromProductsTx(tx *gorm.DB, tagID, projectID string, now time.Time) (err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return merr
	}
	upd := tx.Model(&ProductEntity{}).Where("tag_ids @> ?::jsonb", string(probe))
	if projectID != "" {
		upd = upd.Where("project_id = ?", projectID)
	}
	return upd.UpdateColumns(map[string]any{
		"tag_ids":     gorm.Expr("tag_ids - ?::text", tagID),
		"update_time": now,
	}).Error
}

func (m *Model) ListProductsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) ListProductIDsPublishedSince(ctx context.Context, projectID, status string, since time.Time) (ids []string, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND status = ? AND published_at IS NOT NULL AND published_at >= ?", projectID, status, since).
			Distinct().Pluck("id", &ids).Error
	})
	return ids, err
}

func (m *Model) ListProductIDsByVariantPrice(ctx context.Context, min, max *float64) (ids []string, err error) {
	q := m.VariantDB(ctx).Where("enabled = ?", true)
	if min != nil {
		q = q.Where("price >= ?", *min)
	}
	if max != nil {
		q = q.Where("price <= ?", *max)
	}
	err = q.Distinct().Pluck("product_id", &ids).Error
	return ids, err
}

func (m *Model) ListProductIDsWithDiscount(ctx context.Context) (ids []string, err error) {
	err = m.VariantDB(ctx).
		Where("enabled = ? AND compare_price IS NOT NULL AND compare_price > price", true).
		Distinct().Pluck("product_id", &ids).Error
	return ids, err
}
