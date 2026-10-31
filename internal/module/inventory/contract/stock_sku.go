package inventorycontract

import "context"

type StockSKUReader interface {
	BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error)
}
