package productmodel

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (m *Model) ListVariantsForUpdateTx(ctx context.Context, tx *gorm.DB, productID string) (rows []*VariantEntity, err error) {
	err = tx.WithContext(ctx).Where("product_id = ?", productID).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error
	return rows, err
}

func (m *Model) ListAttributesByIDsForShareTx(ctx context.Context, tx *gorm.DB, ids []string, projectID string) (rows []*ProductAttributeEntity, err error) {
	if len(ids) == 0 {
		return []*ProductAttributeEntity{}, nil
	}
	err = tx.WithContext(ctx).Where("project_id = ? AND id IN ?", projectID, ids).Order("id ASC").Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error
	return rows, err
}
