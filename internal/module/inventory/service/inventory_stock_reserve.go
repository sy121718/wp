package inventoryservice

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

func (s *Service) ReserveStock(ctx context.Context, req *inventorydto.ReserveStockReq) (res *inventorydto.StockReserveResp, err error) {
	out, err := s.reserveStockOn(ctx, nil, req, resolveReserveOp)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) ReserveStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ReserveStockReq) (err error) {
	_, err = s.reserveStockOn(ctx, tx, req, resolveReserveOp)
	return err
}

func (s *Service) ReleaseStock(ctx context.Context, req *inventorydto.ReserveStockReq) (res *inventorydto.StockReserveResp, err error) {
	return s.reserveStockOn(ctx, nil, req, resolveReleaseOp)
}

func (s *Service) ReleaseStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ReserveStockReq) (err error) {
	_, err = s.reserveStockOn(ctx, tx, req, resolveReleaseOp)
	return err
}

type reserveOp struct {
	sign int

	insufficient string
}

var (
	resolveReserveOp = reserveOp{sign: +1, insufficient: inventoryenums.ErrStockInsufficient}
	resolveReleaseOp = reserveOp{sign: -1, insufficient: inventoryenums.ErrReserveUnderflow}
)

func (s *Service) reserveStockOn(ctx context.Context, tx *gorm.DB,
	req *inventorydto.ReserveStockReq, op reserveOp) (out *inventorydto.StockReserveResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if len(req.Lines) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	targets, err := s.reserveTargets(ctx, projectID, req.Lines)
	if err != nil {
		return nil, err
	}
	apply := func(t *gorm.DB) error {
		res, aerr := s.applyReservations(ctx, t, projectID, targets, op)
		if aerr != nil {
			return aerr
		}
		out = res
		return nil
	}
	if tx != nil {
		return out, apply(tx)
	}
	if err = s.m.Transaction(ctx, apply); err != nil {
		return nil, err
	}
	return out, nil
}

type reserveTarget struct {
	key       stockKey
	warehouse *inventorymodel.WarehouseEntity
	variantID string
	skuCode   string
	delta     int
}

func (s *Service) reserveTargets(ctx context.Context, projectID string,
	lines []inventorydto.ReserveStockLine) (targets []*reserveTarget, err error) {
	byKey := make(map[stockKey]*reserveTarget, len(lines))
	order := make([]stockKey, 0, len(lines))
	for i := range lines {
		line := lines[i]
		variantID := strings.TrimSpace(line.VariantID)
		if variantID == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
		}
		if line.Quantity <= 0 {
			return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
		}
		wh, werr := s.resolveWarehouse(ctx, projectID, strings.TrimSpace(line.WarehouseID))
		if werr != nil {
			return nil, werr
		}

		key, serr := s.resolveLineStockKey(ctx, projectID, wh, variantID, line.SKUCode)
		if serr != nil {
			return nil, serr
		}
		if t, ok := byKey[key]; ok {
			t.delta += line.Quantity
			continue
		}
		byKey[key] = &reserveTarget{
			key: key, warehouse: wh, variantID: variantID,
			skuCode: key.skuCode,
			delta:   line.Quantity,
		}
		order = append(order, key)
	}

	sorted := make([]stockKey, len(order))
	copy(sorted, order)
	slices.SortFunc(sorted, func(a, b stockKey) int {
		return cmp.Or(cmp.Compare(a.warehouseID, b.warehouseID), cmp.Compare(a.skuCode, b.skuCode))
	})
	targets = make([]*reserveTarget, 0, len(sorted))
	for _, k := range sorted {
		targets = append(targets, byKey[k])
	}
	return targets, nil
}

func (s *Service) applyReservations(ctx context.Context, tx *gorm.DB, projectID string,
	targets []*reserveTarget, op reserveOp) (out *inventorydto.StockReserveResp, err error) {
	now := time.Now().UTC()
	out = &inventorydto.StockReserveResp{
		ProjectID: projectID,
		Lines:     make([]*inventorydto.StockReserveLineResp, 0, len(targets)),
	}
	for _, t := range targets {
		variantID := t.variantID
		row, lerr := s.m.LockStockBySKUTx(ctx, tx, projectID, t.key.warehouseID, t.key.skuCode)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {

				return nil, inventoryenums.Err(inventoryenums.ErrStockNotFound)
			}
			return nil, lerr
		}
		if !row.TrackQuantity {

			continue
		}
		delta := op.sign * t.delta
		affected, uerr := s.m.UpdateStockReservedTx(ctx, tx, row.ID, delta, now)
		if uerr != nil {
			return nil, uerr
		}
		if affected == 0 {

			return nil, inventoryenums.Err(op.insufficient)
		}
		remaining := row.ReservedQuantity + delta
		out.Lines = append(out.Lines, &inventorydto.StockReserveLineResp{
			StockID: row.ID, WarehouseID: row.WarehouseID,
			WarehouseCode:     t.warehouse.Code,
			VariantID:         variantID,
			SKUCode:           orString(t.skuCode, row.SKUCode),
			Delta:             delta,
			ReservedQuantity:  remaining,
			Quantity:          row.Quantity,
			AvailableQuantity: availableOf(row.Quantity, remaining),
		})
	}
	return out, nil
}

func availableOf(quantity, reserved int) int {
	if v := quantity - reserved; v > 0 {
		return v
	}
	return 0
}

func (s *Service) StockAlert(ctx context.Context, projectID string) (res *inventorydto.StockAlertResp, err error) {
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	row, err := s.m.StockAlertCounts(ctx, pid)
	if err != nil {
		return nil, err
	}
	return &inventorydto.StockAlertResp{
		ProjectID: pid, Total: row.Total,
		OutOfStock: row.OutOfStock, LowStock: row.LowStock,
		Alert: row.OutOfStock + row.LowStock,
	}, nil
}
