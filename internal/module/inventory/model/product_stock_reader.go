package inventorymodel

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
)

var _ inventorycontract.ProductStockReader = (*Model)(nil)

// StockFactsByStockIDs 按货行 id 批量取只读事实（641 起的寻址单位）。
//
// 商品侧手里有的是变体引用的货行 id 清单（product_variants.warehouse_skus），
// 这里只回答「这些行各自是什么」—— 归属求和由商品侧做，因为同一条货行可以被
// 多个变体引用，库存侧按变体求和只会漏算或重复算。
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
