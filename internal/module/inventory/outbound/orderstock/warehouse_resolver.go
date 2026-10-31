package orderstock

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	ordercontract "go_wp/internal/module/order/contract"
)

type WarehouseResolver struct {
	svc inventorycontract.ProductStockLookup
}

var _ ordercontract.WarehouseResolver = (*WarehouseResolver)(nil)

func NewWarehouseResolver(svc inventorycontract.ProductStockLookup) *WarehouseResolver {
	return &WarehouseResolver{svc: svc}
}

func (r *WarehouseResolver) ResolveShipWarehouse(ctx context.Context, projectID, requested string) (string, error) {
	ref, err := r.svc.ResolveWarehouse(ctx, projectID, requested)
	if err != nil {

		if isNoWarehouse(err) {
			return "", nil
		}
		return "", err
	}
	if ref == nil {
		return "", nil
	}
	return ref.ID, nil
}

func isNoWarehouse(err error) bool {
	return err != nil && isDefaultMissing(err)
}

func isDefaultMissing(err error) bool {
	return err != nil && err.Error() == "ErrWarehouseDefaultMissing"
}
