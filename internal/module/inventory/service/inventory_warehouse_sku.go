package inventoryservice

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	i18n "go_wp/pkg/i18n"
	"go_wp/pkg/money"
)

const (
	maxStockNameLen     = 200
	maxStockBarcodeLen  = 64
	maxStockRemarkLen   = 500
	maxStockImageURLLen = 512
)

func (s *Service) UpdateStock(ctx context.Context, req *inventorydto.UpdateStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	status := strings.TrimSpace(req.Status)
	if status != "" && status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
		return nil, inventoryenums.Err(inventoryenums.ErrStockStatusInvalid)
	}
	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > maxStockNameLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockNameTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockNameLimit, "max", strconv.Itoa(maxStockNameLen)))
	}
	barcode := strings.TrimSpace(req.Barcode)
	if len(barcode) > maxStockBarcodeLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockBarcodeTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockBarcodeLimit, "max", strconv.Itoa(maxStockBarcodeLen)))
	}
	remark := strings.TrimSpace(req.Remark)
	if len([]rune(remark)) > maxStockRemarkLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockRemarkTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockRemarkLimit, "max", strconv.Itoa(maxStockRemarkLen)))
	}
	imageURL := strings.TrimSpace(req.ImageURL)
	if len(imageURL) > maxStockImageURLLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockImageTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockImageLimit, "max", strconv.Itoa(maxStockImageURLLen)))
	}
	cost, err := normalizeExplicitCost(req.CostPrice)
	if err != nil {
		return nil, err
	}
	freight, err := normalizeFreight(req.FreightPrice)
	if err != nil {
		return nil, err
	}
	fx, err := normalizeFXRate(req.CostFXRate)
	if err != nil {
		return nil, err
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}

	externalSKU, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	var out *inventorymodel.StockEntity
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		row, lerr := s.m.GetStockScopedTx(ctx, tx, req.ID, projectID)
		if lerr != nil {
			return mapStockNotFound(lerr)
		}
		if aerr := s.assertExternalSKUProductScope(ctx, projectID, row.WarehouseID, externalSKU,
			inventorymodel.PtrString(row.ProductID)); aerr != nil {
			return aerr
		}
		costCurrency := strings.TrimSpace(req.CostCurrency)
		if costCurrency == "" {
			costCurrency = row.CostCurrency
		}
		baseCurrency := strings.TrimSpace(req.CostBaseCurrency)
		if baseCurrency == "" {
			baseCurrency = costCurrency
		}
		if fx == nil && !strings.EqualFold(costCurrency, baseCurrency) {

			fx = nil
		} else if fx == nil && strings.EqualFold(costCurrency, baseCurrency) {
			fx = nil
		}
		patch := map[string]any{
			"name":               name,
			"image_url":          imageURL,
			"barcode":            barcode,
			"remark":             remark,
			"external_sku":       externalSKU,
			"cost_price":         cost,
			"freight_price":      freightPriceOr(row.FreightPrice, freight),
			"cost_currency":      costCurrency,
			"cost_base_currency": baseCurrency,
			"cost_fx_rate":       fx,
			"update_time":        time.Now().UTC(),
		}
		if status != "" {
			patch["status"] = status
		}
		if uerr := s.m.UpdateStockFieldsTx(ctx, tx, req.ID, projectID, patch); uerr != nil {
			return uerr
		}
		row2, gerr := s.m.GetStockScopedTx(ctx, tx, req.ID, projectID)
		if gerr != nil {
			return mapStockNotFound(gerr)
		}
		out = row2
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, out), nil
}

func (s *Service) UpdateStockStatus(ctx context.Context, req *inventorydto.UpdateStockStatusReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	status := strings.TrimSpace(req.Status)
	if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
		return nil, inventoryenums.Err(inventoryenums.ErrStockStatusInvalid)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	var out *inventorymodel.StockEntity
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.m.UpdateStockFieldsTx(ctx, tx, req.ID, projectID, map[string]any{
			"status":      status,
			"update_time": time.Now().UTC(),
		}); uerr != nil {
			return uerr
		}
		row, gerr := s.m.GetStockScopedTx(ctx, tx, req.ID, projectID)
		if gerr != nil {
			return mapStockNotFound(gerr)
		}
		out = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, out), nil
}

func (s *Service) CountWarehouseSKUsByFilter(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (total int64, err error) {
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return 0, err
	}
	return s.m.CountWarehouseSKUs(ctx, inventorymodel.WarehouseSKUFilter{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(listWarehouseSKUWarehouseID(req)),
		Keyword:     strings.TrimSpace(listWarehouseSKUKeyword(req)),
		Status:      strings.TrimSpace(listWarehouseSKUStatus(req)),
	})
}

func normalizeFreight(p *float64) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	if *p < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockFreightInvalid)
	}
	return p, nil
}

func normalizeFXRate(p *float64) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	if *p <= 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockFXRateInvalid)
	}
	return p, nil
}

func freightPriceOr(current, next *float64) *float64 {
	if next == nil {
		return current
	}
	return next
}

const usageShownPerRow = 3

func (s *Service) ListStockSKURows(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (rows []inventorydto.StockSKURowResp, err error) {
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return nil, err
	}
	scoped := inventorydto.ListWarehouseSKUReq{
		ProjectID:   projectID,
		WarehouseID: listWarehouseSKUWarehouseID(req),
		Keyword:     listWarehouseSKUKeyword(req),
		Status:      listWarehouseSKUStatus(req),
		Page:        req.Page,
		Size:        req.Size,
	}
	list, err := s.ListWarehouseSKUs(ctx, &scoped)
	if err != nil {
		return nil, err
	}
	out := make([]inventorydto.StockSKURowResp, 0, len(list))
	if len(list) == 0 {
		return out, nil
	}
	stockIDs := make([]string, 0, len(list))
	for _, r := range list {
		if r != nil && r.ID != "" {
			stockIDs = append(stockIDs, r.ID)
		}
	}
	byStock, codes := s.usageOf(ctx, projectID, stockIDs)
	for _, r := range list {
		if r == nil {
			continue
		}
		row := inventorydto.StockSKURowResp{
			ID: r.ID, WarehouseID: r.WarehouseID,
			WarehouseName: r.WarehouseName, WarehouseCode: r.WarehouseCode, IsDefault: r.IsDefault,
			SKUCode: r.SKUCode, ExternalSKU: r.ExternalSKU, Barcode: r.Barcode,
			Name: r.Name, ImageURL: r.ImageURL,
			ReservedQuantity: r.ReservedQuantity, AvailableQuantity: r.AvailableQuantity,
			TrackQuantity: r.TrackQuantity,
			CostText:      moneyTextOr(r.CostPrice),
			FreightText:   moneyTextOr(r.FreightPrice),
			CurrencyText:  currencyPairText(r.CostCurrency, r.CostBaseCurrency),
			FXText:        fxTextOf(r.CostFXRate, r.CostCurrency, r.CostBaseCurrency),
			FXMissing:     isFXMissing(r.CostFXRate, r.CostCurrency, r.CostBaseCurrency),
			Status:        r.Status,
			Remark:        r.Remark,
			UpdatedAt:     r.UpdatedAt,
		}

		if r.TrackQuantity {
			row.QuantityText = strconv.Itoa(r.Quantity)
		}
		ids := byStock[r.ID]
		row.VariantCount = len(ids)
		for i, id := range ids {
			if i >= usageShownPerRow {
				break
			}
			row.Variants = append(row.Variants, inventorydto.StockVariantUsage{
				VariantID: id, SKUCode: codes[id],
			})
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *Service) usageOf(ctx context.Context, projectID string, stockIDs []string) (byStock map[string][]string, codes map[string]string) {
	byStock, codes = map[string][]string{}, map[string]string{}
	if s.refs == nil || projectID == "" || len(stockIDs) == 0 {
		return byStock, codes
	}
	byStock, err := s.refs.VariantIDsByWarehouseStocks(ctx, projectID, stockIDs)
	if err != nil {
		return map[string][]string{}, codes
	}
	variantIDs := make([]string, 0, len(byStock))
	for _, ids := range byStock {
		variantIDs = append(variantIDs, ids...)
	}
	if codeMap, cerr := s.refs.VariantSKUCodes(ctx, projectID, variantIDs); cerr == nil {
		codes = codeMap
	}
	return byStock, codes
}

func moneyTextOr(v *float64) string { return money.FormatYuanOrDash(v) }

func currencyPairText(currency, base string) string {
	currency = strings.TrimSpace(currency)
	base = strings.TrimSpace(base)
	if currency == "" {
		return ""
	}
	if base == "" || strings.EqualFold(currency, base) {
		return currency
	}
	return currency + " → " + base
}

func fxTextOf(rate *float64, currency, base string) string {
	if rate != nil && *rate > 0 {
		return strconv.FormatFloat(*rate, 'f', -1, 64)
	}

	return ""
}

func isFXMissing(rate *float64, currency, base string) bool {
	if rate != nil && *rate > 0 {
		return false
	}
	currency = strings.TrimSpace(currency)
	base = strings.TrimSpace(base)
	if currency == "" || base == "" {
		return false
	}
	return !strings.EqualFold(currency, base)
}
