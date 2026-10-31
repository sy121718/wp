package orderstock

import (
	"context"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryservice "go_wp/internal/module/inventory/service"
	ordercontract "go_wp/internal/module/order/contract"
)

type Operator struct {
	svc *inventoryservice.Service
}

var _ ordercontract.StockOperator = (*Operator)(nil)

func New(svc *inventoryservice.Service) *Operator {
	return &Operator{svc: svc}
}

func (o *Operator) DeductStock(ctx context.Context, in *ordercontract.StockDeduction) (err error) {
	if in == nil {
		return nil
	}
	_, err = o.svc.DeductStock(ctx, &inventorydto.DeductStockReq{
		ProjectID:  in.ProjectID,
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
	return err
}

func (o *Operator) ChangeStock(ctx context.Context, in *ordercontract.StockAdjustment) (err error) {
	if in == nil {
		return nil
	}
	_, err = o.svc.RestockStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID:  in.ProjectID,
		Direction:  "in",
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
	return err
}

func (o *Operator) DeductStockTx(ctx context.Context, tx *gorm.DB, in *ordercontract.StockDeduction) (err error) {
	if in == nil {
		return nil
	}
	return o.svc.DeductStockTx(ctx, tx, &inventorydto.DeductStockReq{
		ProjectID:  in.ProjectID,
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
}

func (o *Operator) ChangeStockTx(ctx context.Context, tx *gorm.DB, in *ordercontract.StockAdjustment) (err error) {
	if in == nil {
		return nil
	}
	return o.svc.RestockStockTx(ctx, tx, &inventorydto.ChangeStockReq{
		ProjectID:  in.ProjectID,
		Direction:  "in",
		ReasonCode: in.ReasonCode,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      toStockLines(in.Lines),
	})
}

func (o *Operator) ReserveStockTx(ctx context.Context, tx *gorm.DB, in *ordercontract.StockReservation) (err error) {
	if in == nil {
		return nil
	}
	return o.svc.ReserveStockTx(ctx, tx, toReserveReq(in))
}

func (o *Operator) ReleaseStockTx(ctx context.Context, tx *gorm.DB, in *ordercontract.StockReservation) (err error) {
	if in == nil {
		return nil
	}
	return o.svc.ReleaseStockTx(ctx, tx, toReserveReq(in))
}

func (o *Operator) LocateStockRow(ctx context.Context, projectID, variantID, warehouseID string) (string, error) {
	return o.svc.LocateStockRow(ctx, projectID, variantID, warehouseID)
}

func toReserveReq(in *ordercontract.StockReservation) *inventorydto.ReserveStockReq {
	lines := make([]inventorydto.ReserveStockLine, 0, len(in.Lines))
	for _, l := range in.Lines {
		lines = append(lines, inventorydto.ReserveStockLine{
			ProductID:   l.ProductID,
			VariantID:   l.VariantID,
			SKUCode:     l.SKUCode,
			Quantity:    l.Quantity,
			WarehouseID: l.WarehouseID,
		})
	}
	return &inventorydto.ReserveStockReq{
		ProjectID:  in.ProjectID,
		SourceType: in.SourceType,
		SourceRef:  in.SourceRef,
		Remark:     in.Remark,
		Lines:      lines,
	}
}

func toStockLines(lines []ordercontract.StockLine) []inventorydto.StockChangeLineReq {
	if len(lines) == 0 {
		return nil
	}
	out := make([]inventorydto.StockChangeLineReq, 0, len(lines))
	for _, l := range lines {
		out = append(out, inventorydto.StockChangeLineReq{
			WarehouseID: l.WarehouseID,
			ProductID:   l.ProductID,
			VariantID:   l.VariantID,
			SKUCode:     l.SKUCode,
			Quantity:    l.Quantity,
		})
	}
	return out
}
