package purchasestock

import (
	"context"

	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	purchasecontract "go_wp/internal/module/purchase/contract"
)

type Stock struct {
	svc    inventorycontract.InventoryService
	lookup inventorycontract.ProductStockLookup
	batch  inventorycontract.BatchStockWriter
}

var _ purchasecontract.StockPort = (*Stock)(nil)

func New(svc inventorycontract.InventoryService, lookup inventorycontract.ProductStockLookup,
	batch inventorycontract.BatchStockWriter) *Stock {
	return &Stock{svc: svc, lookup: lookup, batch: batch}
}

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

func (s *Stock) ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (*purchasecontract.WarehouseRef, error) {
	ref, err := s.lookup.ResolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	return &purchasecontract.WarehouseRef{
		ID: ref.ID, ProjectID: ref.ProjectID, Code: ref.Code, Name: ref.Name,
	}, nil
}

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
