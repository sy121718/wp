package productmodel

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (m *Model) LockPricingVariantsTx(ctx context.Context, tx *gorm.DB, ids []string) (rows []*VariantEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&VariantEntity{}).Where("id IN ?", ids).
		Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (m *Model) LockPricingTagsTx(ctx context.Context, tx *gorm.DB, projectID, kind string) error {
	var ids []string
	return tx.WithContext(ctx).Model(&ProductTagEntity{}).Where("project_id = ? AND kind = ?", projectID, kind).
		Clauses(clause.Locking{Strength: "UPDATE"}).Order("id ASC").Pluck("id", &ids).Error
}
