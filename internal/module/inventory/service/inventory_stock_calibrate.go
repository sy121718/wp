package inventoryservice

import (
	"context"
	"errors"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"

	"gorm.io/gorm"
)

const calibrateSourceRefPrefix = "product:"

func (s *Service) CalibrateVariantStocksTx(ctx context.Context, tx *gorm.DB, projectID, productID, variantID string,
	targets []inventorycontract.ProductStockTarget) (err error) {
	variantID = strings.TrimSpace(variantID)
	if variantID == "" {
		return inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
	}
	if len(targets) == 0 {
		return nil
	}
	if len(targets) > maxBatchLines {
		return inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	lines := make([]inventorydto.StockChangeLineReq, 0, len(targets))
	for _, t := range targets {
		line, keep, lerr := s.calibrateLine(ctx, pid, productID, variantID, t)
		if lerr != nil {
			return lerr
		}
		if keep {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {

		return nil
	}
	req := &inventorydto.ChangeStockReq{
		ProjectID:  pid,
		Direction:  inventoryenums.DirectionAdjust,
		ReasonCode: inventoryenums.ReasonManualAdjust,
		SourceType: inventoryenums.MovementSourceProductForm,
		SourceRef:  calibrateSourceRefPrefix + strings.TrimSpace(productID),
		Lines:      lines,
	}

	_, _, _, cerr := s.changeStockTrackingTx(ctx, tx, req, nil)
	return cerr
}

func (s *Service) calibrateLine(ctx context.Context, projectID, productID, variantID string,
	t inventorycontract.ProductStockTarget) (line inventorydto.StockChangeLineReq, keep bool, err error) {
	if t.Quantity < 0 {
		return line, false, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
	}
	wh, werr := s.resolveWarehouse(ctx, projectID, t.WarehouseID)
	if werr != nil {
		return line, false, werr
	}

	bare, berr := normalizeStockSKU(strings.TrimSpace(t.SKUCode), wh.Code)
	if berr != nil {
		bare = ""
	}
	row := (*inventorymodel.StockEntity)(nil)
	if bare != "" {
		got, gerr := s.m.GetStockByWarehouseSKU(ctx, wh.ID, bare, projectID)
		switch {
		case gerr == nil:
			row = got
		case errors.Is(gerr, gorm.ErrRecordNotFound):
		default:
			return line, false, gerr
		}
	}
	if row == nil && strings.TrimSpace(t.StockID) != "" {

		got, gerr := s.m.GetStock(ctx, strings.TrimSpace(t.StockID), projectID)
		switch {
		case gerr == nil:
			row = got
			bare = row.SKUCode
		case errors.Is(gerr, gorm.ErrRecordNotFound):
		default:
			return line, false, gerr
		}
	}
	if bare == "" {
		return line, false, inventoryenums.Err(inventoryenums.ErrStockSKURequired)
	}
	if row == nil {

		if t.Quantity == 0 {
			return line, false, nil
		}
	} else {
		if row.Quantity == t.Quantity {

			return line, false, nil
		}

	}
	return inventorydto.StockChangeLineReq{
		WarehouseID: wh.ID,
		ProductID:   strings.TrimSpace(productID),
		VariantID:   variantID,
		SKUCode:     bare,
		Quantity:    t.Quantity,
	}, true, nil
}
