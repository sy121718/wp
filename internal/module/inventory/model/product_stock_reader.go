package inventorymodel

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
)

var _ inventorycontract.ProductStockReader = (*Model)(nil)

// VariantStockTotals returns only the totals needed by the product projection.
func (m *Model) VariantStockTotals(ctx context.Context, projectID string, variantIDs []string) (totals map[string]int, err error) {
	rows, err := m.StockTotals(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	totals = make(map[string]int, len(rows))
	for _, row := range rows {
		if row != nil {
			totals[row.VariantID] = row.Total
		}
	}
	return totals, nil
}

// HasBOMParent avoids exposing inventory persistence entities across the product boundary.
func (m *Model) HasBOMParent(ctx context.Context, variantID, projectID string) (exists bool, err error) {
	rows, err := m.ListBOMParents(ctx, variantID, projectID)
	return len(rows) > 0, err
}
