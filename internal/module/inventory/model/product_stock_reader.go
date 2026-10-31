package inventorymodel

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	inventorycontract "go_wp/internal/module/inventory/contract"
)

var _ inventorycontract.ProductStockReader = (*Model)(nil)

func (m *Model) StockFactsByStockIDs(ctx context.Context, projectID string, stockIDs []string) (facts []*inventorycontract.StockFact, err error) {
	rows, err := m.ListStockFactsByIDs(ctx, projectID, stockIDs)
	if err != nil {
		return nil, err
	}
	facts = make([]*inventorycontract.StockFact, 0, len(rows))
	for _, e := range rows {
		if e == nil {
			continue
		}
		facts = append(facts, &inventorycontract.StockFact{
			StockID:          e.ID,
			WarehouseID:      e.WarehouseID,
			WarehouseCode:    e.WarehouseCode,
			WarehouseName:    e.WarehouseName,
			SKUCode:          e.SKUCode,
			Quantity:         e.Quantity,
			ReservedQuantity: e.ReservedQuantity,
			TrackQuantity:    e.TrackQuantity,
			CostPrice:        e.CostPrice,
			Name:             e.Name,
			ImageURL:         e.ImageURL,
		})
	}
	return facts, nil
}

func (m *Model) VariantDeletionFactsTx(ctx context.Context, tx *gorm.DB, projectID string, variantIDs, stockIDs []string) (*inventorycontract.VariantDeletionFacts, error) {
	result := &inventorycontract.VariantDeletionFacts{}
	if len(stockIDs) > 0 {
		rows := []*StockEntity{}
		if err := tx.WithContext(ctx).Where("project_id = ? AND id IN ?", projectID, stockIDs).Order("warehouse_id ASC, sku_code ASC, id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			result.Stocks = append(result.Stocks, &inventorycontract.StockFact{StockID: row.ID, Quantity: row.Quantity, ReservedQuantity: row.ReservedQuantity, TrackQuantity: row.TrackQuantity})
		}
	}
	if len(variantIDs) > 0 {
		if err := tx.WithContext(ctx).Table("inventory_stock_movements").Where("project_id = ? AND variant_id IN ?", projectID, variantIDs).Distinct("variant_id").Pluck("variant_id", &result.MovementVariantIDs).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}
