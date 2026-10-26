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
