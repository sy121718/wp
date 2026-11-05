package productmodel

import (
	"context"
	json "go_wp/pkg/jsonx"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

type ProductAttributeEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;not null"`
	Key         string          `gorm:"column:key;not null"`
	Name        string          `gorm:"column:name;not null"`
	IsVariation bool            `gorm:"column:is_variation;not null"`
	Sort        int             `gorm:"column:sort;not null"`
	Values      json.RawMessage `gorm:"column:values;type:jsonb;not null"`
	Metadata    json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time       `gorm:"column:update_time;not null"`
}

func (ProductAttributeEntity) TableName() string { return "product_attributes" }

func (m *Model) AttributeDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductAttributeEntity{})
}

func (m *Model) GetAttribute(ctx context.Context, id, projectID string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

func (m *Model) GetAttributeWithoutScope(ctx context.Context, id string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = m.AttributeDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

func (m *Model) AttributeKeyExists(ctx context.Context, projectID, key, excludeID string) (exists bool, err error) {

	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{}).
			Where("project_id = ? AND key = ?", projectID, key)
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
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (m *Model) ListAttributes(ctx context.Context, projectID, keyword string, variation *bool, limit, offset int) (list []*ProductAttributeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{})
		if keyword != "" {
			q = q.Where(`(name ILIKE ? ESCAPE '\' OR key ILIKE ? ESCAPE '\')`, literalKeywordPattern(keyword), literalKeywordPattern(keyword))
		}
		if variation != nil {
			q = q.Where("is_variation = ?", *variation)
		}
		return q.Order("sort ASC, create_time ASC, id ASC").Limit(limit).Offset(offset).Find(&list).Error
	})
	return list, err
}

func (m *Model) CountAttributes(ctx context.Context, projectID, keyword string, variation *bool) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{})
		if keyword != "" {
			q = q.Where(`(name ILIKE ? ESCAPE '\' OR key ILIKE ? ESCAPE '\')`, literalKeywordPattern(keyword), literalKeywordPattern(keyword))
		}
		if variation != nil {
			q = q.Where("is_variation = ?", *variation)
		}
		return q.Count(&n).Error
	})
	return n, err
}

func (m *Model) ListAttributesByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductAttributeEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

func (m *Model) ListAttributesByProject(ctx context.Context, projectID string) (list []*ProductAttributeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).
			Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

func (m *Model) CreateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductAttributeEntity{}).Create(e).Error
	})
}

func (m *Model) UpdateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductAttributeEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

func (m *Model) DeleteAttribute(ctx context.Context, id string) (err error) {
	return m.AttributeDB(ctx).Where("id = ?", id).Delete(&ProductAttributeEntity{}).Error
}

func (m *Model) CreateAttributeTx(ctx context.Context, tx *gorm.DB, e *ProductAttributeEntity) error {
	return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Create(e).Error
}

func (m *Model) UpdateAttributeTx(ctx context.Context, tx *gorm.DB, e *ProductAttributeEntity) error {
	return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteAttributeTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", id).Delete(&ProductAttributeEntity{}).Error
}

func (m *Model) ListProductAttributeIDs(ctx context.Context, productID, projectID string) (raw json.RawMessage, err error) {
	var row struct {
		AttributeIDs json.RawMessage `gorm:"column:attribute_ids"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("attribute_ids").Where("id = ?", productID).Take(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return row.AttributeIDs, nil
}

func (m *Model) ProductUsingAttribute(ctx context.Context, attributeID, projectID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("attribute_ids @> ?::jsonb", string(probe)).
			Order("sort ASC, create_time ASC, id ASC").Limit(1).Take(e).Error
	})
	return e, err
}

func (m *Model) GetAttributeForUpdateTx(ctx context.Context, tx *gorm.DB, id, projectID string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = tx.WithContext(ctx).Model(&ProductAttributeEntity{}).
		Where("id = ? AND project_id = ?", id, projectID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(e).Error
	return e, err
}
