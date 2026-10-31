package orderstock

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	ordercontract "go_wp/internal/module/order/contract"
	productcontract "go_wp/internal/module/product/contract"
)

type ShipProbe struct {
	stocks inventorycontract.ProductStockLookup
	refs   productcontract.VariantWarehouseStockPort
}

var _ ordercontract.ShipStockProbe = (*ShipProbe)(nil)

func NewShipProbe(stocks inventorycontract.ProductStockLookup, refs productcontract.VariantWarehouseStockPort) *ShipProbe {
	return &ShipProbe{stocks: stocks, refs: refs}
}

func (p *ShipProbe) ShipAvailabilityByLine(ctx context.Context, projectID string,
	lines []ordercontract.ShipProbeLine) (map[uint64]ordercontract.ShipAvailability, error) {
	out := make(map[uint64]ordercontract.ShipAvailability, len(lines))
	if len(lines) == 0 {
		return out, nil
	}
	variantIDs := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.VariantID != "" {
			variantIDs = append(variantIDs, line.VariantID)
		}
	}

	byVariant := map[string][]string{}
	skuOf := map[string]string{}
	if len(variantIDs) > 0 && p.refs != nil {
		ids, err := p.refs.WarehouseStockIDsByVariant(ctx, projectID, variantIDs)
		if err != nil {
			return nil, err
		}
		byVariant = ids
		skus, serr := p.refs.VariantSKUCodes(ctx, projectID, variantIDs)
		if serr != nil {
			return nil, serr
		}
		skuOf = skus
	}
	stockIDs := make([]string, 0, len(variantIDs))
	for _, ids := range byVariant {
		stockIDs = append(stockIDs, ids...)
	}
	facts := map[string][]*inventorycontract.StockFact{}
	if len(stockIDs) > 0 {
		list, err := p.stocks.StockFactsByStockIDs(ctx, projectID, stockIDs)
		if err != nil {
			return nil, err
		}
		for _, f := range list {
			if f == nil {
				continue
			}
			facts[f.StockID] = append(facts[f.StockID], f)
		}
	}

	defaultWarehouse, resolved := "", false
	for _, line := range lines {
		avail := ordercontract.ShipAvailability{}
		switch {
		case line.VariantID == "":

			out[line.OrderItemID] = avail
			continue
		case len(byVariant[line.VariantID]) == 0 && skuOf[line.VariantID] == "":

			avail.Unmatched = true
			out[line.OrderItemID] = avail
			continue
		}
		warehouseID := line.WarehouseID
		if warehouseID == "" {
			if !resolved {
				resolved = true
				ref, err := p.stocks.ResolveWarehouse(ctx, projectID, "")
				if err != nil && !isNoWarehouse(err) {
					return nil, err
				}
				if ref != nil {
					defaultWarehouse = ref.ID
				}
			}
			warehouseID = defaultWarehouse
			if warehouseID == "" {
				avail.NoWarehouse = true
				out[line.OrderItemID] = avail
				continue
			}
		}
		tracked, total := 0, 0
		for _, id := range byVariant[line.VariantID] {
			for _, f := range facts[id] {
				if f.WarehouseID != warehouseID {
					continue
				}
				tracked++
				total += f.Quantity
			}
		}
		if tracked == 0 {

			avail.Infinite = true
		}
		avail.Available = total
		out[line.OrderItemID] = avail
	}
	return out, nil
}
