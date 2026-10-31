package orderstock

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	ordercontract "go_wp/internal/module/order/contract"
)

type WarehouseSource struct {
	svc inventorycontract.InventoryService
}

var _ ordercontract.ReturnWarehouseSource = (*WarehouseSource)(nil)

func NewWarehouseSource(svc inventorycontract.InventoryService) *WarehouseSource {
	return &WarehouseSource{svc: svc}
}

func (w *WarehouseSource) ListReturnWarehouses(ctx context.Context, projectID string) ([]ordercontract.ReturnWarehouse, error) {
	list, err := w.svc.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	out := make([]ordercontract.ReturnWarehouse, 0, len(list))
	for _, wh := range list {
		if wh == nil {
			continue
		}
		out = append(out, ordercontract.ReturnWarehouse{
			ID:        wh.ID,
			Name:      wh.Name,
			Code:      wh.Code,
			Status:    wh.Status,
			IsDefault: wh.IsDefault,
		})
	}
	return out, nil
}
