// Package purchasestock 采购侧的库存适配器（CQ-004：适配器在库存侧）。
//
// 采购模块持有 purchasecontract.StockPort 这个窄端口，库存数据以采购契约的
// 自有视图类型（purchasecontract.WarehouseRef）交付 —— purchase 因此不需要 import
// 库存的 dto。与 orderstock（订单侧）同一手法、同一住处。
package purchasestock

import (
	"context"

	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	purchasecontract "go_wp/internal/module/purchase/contract"
)

// Stock 实现采购侧的库存端口。
type Stock struct {
	svc    inventorycontract.InventoryService
	lookup inventorycontract.ProductStockLookup
	batch  inventorycontract.BatchStockWriter
}

// 编译期断言：适配器必须满足采购侧的库存端口（与 orderstock 同一手法）。
var _ purchasecontract.StockPort = (*Stock)(nil)

// New 构造；svc 为库存契约（仓解析与仓名映射），batch 为带批次号的变动口。
func New(svc inventorycontract.InventoryService, lookup inventorycontract.ProductStockLookup,
	batch inventorycontract.BatchStockWriter) *Stock {
	return &Stock{svc: svc, lookup: lookup, batch: batch}
}

// ChangeStockTx 把采购侧的变动形状翻译成库存的 ChangeStockReq，并在调用方事务内执行。
func (s *Stock) ChangeStockTx(ctx context.Context, tx *gorm.DB, req *purchasecontract.StockChange) (string, error) {
	if req == nil {
		return "", nil
	}
	lines := make([]inventorydto.StockChangeLineReq, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, inventorydto.StockChangeLineReq{
			WarehouseID: l.WarehouseID,
			ProductID:   l.ProductID,
			VariantID:   l.VariantID,
			SKUCode:     l.SKUCode,
			Quantity:    l.Quantity,
			CostPrice:   l.CostPrice,
		})
	}
	return s.batch.ChangeStockTxWithBatch(ctx, tx, &inventorydto.ChangeStockReq{
		ProjectID:  req.ProjectID,
		Direction:  req.Direction,
		ReasonCode: req.ReasonCode,
		SourceType: req.SourceType,
		SourceRef:  req.SourceRef,
		Remark:     req.Remark,
		OperatorID: req.OperatorID,
		Lines:      lines,
	})
}

// ResolveWarehouse 解析收货仓（显式 id 需同工程且启用；为空兜底工程默认仓）。
func (s *Stock) ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (*purchasecontract.WarehouseRef, error) {
	ref, err := s.lookup.ResolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	return &purchasecontract.WarehouseRef{
		ID: ref.ID, ProjectID: ref.ProjectID, Code: ref.Code, Name: ref.Name,
	}, nil
}

// Warehouses 工程内的仓库清单（收货仓下拉：要短码与「是否默认仓」）。
func (s *Stock) Warehouses(ctx context.Context, projectID string) ([]purchasecontract.WarehouseOption, error) {
	list, err := s.svc.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	out := make([]purchasecontract.WarehouseOption, 0, len(list))
	for _, wh := range list {
		if wh == nil {
			continue
		}
		out = append(out, purchasecontract.WarehouseOption{
			ID: wh.ID, Code: wh.Code, Name: wh.Name, IsDefault: wh.IsDefault,
		})
	}
	return out, nil
}

// WarehouseNames 工程内「仓库 id → 名称」的一次性映射（列表补名，不产生 N+1）。
func (s *Stock) WarehouseNames(ctx context.Context, projectID string) (map[string]string, error) {
	list, err := s.svc.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, wh := range list {
		if wh == nil {
			continue
		}
		out[wh.ID] = wh.Name
	}
	return out, nil
}
