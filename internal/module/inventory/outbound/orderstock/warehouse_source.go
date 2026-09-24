package orderstock

import (
	"context"

	ordercontract "go_wp/internal/module/order/contract"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
)

// WarehouseSource 退货页「入库仓库」下拉的库存侧适配器（CQ-004：适配器在库存侧）。
//
// 订单侧持有 ordercontract.ReturnWarehouseSource 这个窄端口，仓库数据以订单契约的
// 自有视图类型（ordercontract.ReturnWarehouse）交付 —— 订单模块因此不需要 import
// 库存的 dto。与 Operator（StockOperator 的库存侧实现）同住一个出站包。
type WarehouseSource struct {
	svc inventorycontract.InventoryService
}

// NewWarehouseSource 构造；svc 为库存契约（只用到仓库列表一条能力）。
func NewWarehouseSource(svc inventorycontract.InventoryService) *WarehouseSource {
	return &WarehouseSource{svc: svc}
}

// ListReturnWarehouses 某工程的仓库视图列表（默认仓在最前，由库存侧排序保证）。
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
