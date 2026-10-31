package inventoryservice

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	i18n "go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
)

func (s *Service) CreateStock(ctx context.Context, req *inventorydto.CreateStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if req.Quantity < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
	}
	if req.Quantity > 0 && !req.TrackQuantity {

		return nil, inventoryenums.Err(inventoryenums.ErrStockUntrackedQuantity)
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
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	wh, err := s.resolveWarehouse(ctx, projectID, strings.TrimSpace(req.WarehouseID))
	if err != nil {
		return nil, err
	}
	skuCode, err := normalizeStockSKU(strings.TrimSpace(req.SKUCode), wh.Code)
	if err != nil {
		return nil, err
	}
	externalSKU, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	if err = s.assertExternalSKUProductScope(ctx, projectID, wh.ID, externalSKU, ""); err != nil {
		return nil, err
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
	exists, err := s.m.StockSKUCodeExists(ctx, projectID, wh.ID, skuCode, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseSKUCodeTaken)
	}
	costCurrency, baseCurrency := normalizeCostCurrencies(req.CostCurrency, req.CostBaseCurrency)
	now := time.Now().UTC()
	row := &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: wh.ProjectID, WarehouseID: wh.ID,
		SKUCode: skuCode, ExternalSKU: externalSKU,
		TrackQuantity: req.TrackQuantity, Quantity: req.Quantity,
		CostPrice: cost,

		Name: name, ImageURL: imageURL, Barcode: barcode, Remark: remark,

		FreightPrice: freight, CostFXRate: fx,
		CostCurrency: costCurrency, CostBaseCurrency: baseCurrency,
		Status:   statusOf(req.Status),
		Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateStockRowTx(ctx, tx, row); cerr != nil {
			return cerr
		}

		res = s.toStockResp(ctx, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func normalizeCostCurrencies(currency, base string) (out, baseOut string) {
	out = strings.ToUpper(strings.TrimSpace(currency))
	if out == "" {
		out = defaultCostCurrency
	}
	baseOut = strings.ToUpper(strings.TrimSpace(base))
	if baseOut == "" {
		baseOut = out
	}
	return out, baseOut
}

func statusOf(status string) string {
	if v := strings.TrimSpace(status); v != "" {
		return v
	}
	return inventoryenums.StatusActive
}

const defaultCostCurrency = "CNY"
