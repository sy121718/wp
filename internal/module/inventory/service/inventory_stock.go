package inventoryservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) EnsureStock(ctx context.Context, req *inventorydto.EnsureStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.VariantID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
	}

	e, err := s.ensureStockRow(ctx, req.ProjectID, req.WarehouseID,
		strings.TrimSpace(req.ProductID), strings.TrimSpace(req.VariantID), strings.TrimSpace(req.SKUCode), nil)
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, e), nil
}

func (s *Service) ensureStockRow(ctx context.Context, projectID, warehouseID, productID, variantID, skuCode string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	return s.ensureStockRowWithExternalTx(ctx, nil, projectID, warehouseID, productID, variantID, skuCode, "", quantity)
}

func (s *Service) ensureStockRowTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, productID, variantID, skuCode string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	return s.ensureStockRowWithExternalTx(ctx, tx, projectID, warehouseID, productID, variantID, skuCode, "", quantity)
}

func (s *Service) ensureStockRowWithExternal(ctx context.Context, projectID, warehouseID, productID, variantID, skuCode, externalSKU string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	return s.ensureStockRowWithExternalTx(ctx, nil, projectID, warehouseID, productID, variantID, skuCode, externalSKU, quantity)
}

func (s *Service) ensureStockRowWithExternalTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, productID, variantID, skuCode, externalSKU string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: wh.ProjectID, WarehouseID: wh.ID,
		ProductID: inventorymodel.StringPtr(productID), VariantID: inventorymodel.StringPtr(variantID),
		SKUCode:     skuCode,
		ExternalSKU: externalSKU,

		TrackQuantity: false,
		Quantity:      0, Metadata: json.RawMessage("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if quantity != nil {
		if *quantity < 0 {
			return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
		}

		row.TrackQuantity = true
		row.Quantity = *quantity
	}
	if tx != nil {

		return s.m.EnsureStockTx(ctx, tx, row)
	}
	return s.m.EnsureStock(ctx, row)
}

func (s *Service) GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	var e *inventorymodel.StockEntity
	if strings.TrimSpace(req.ID) != "" {
		if e, err = s.m.GetStock(ctx, req.ID, projectID); err != nil {
			return nil, mapStockNotFound(err)
		}
	} else if strings.TrimSpace(req.VariantID) != "" && strings.TrimSpace(req.WarehouseID) != "" {
		if e, err = s.m.GetStockByVariantWarehouse(ctx, req.VariantID, req.WarehouseID, projectID); err != nil {
			return nil, mapStockNotFound(err)
		}
	} else {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	return s.toStockResp(ctx, e), nil
}

func (s *Service) ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.SKUCode) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListStockRows(ctx, inventorymodel.StockFilter{
		ProjectID: projectID, SKUCode: strings.TrimSpace(req.SKUCode),
	}, 0, 0)
	if err != nil {
		return nil, err
	}
	return toStockRespList(rows), nil
}

func (s *Service) BareSKUsByVariant(ctx context.Context, projectID string) (map[string]string, error) {
	pid, err := s.resolveProjectID(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	if s.refs == nil {

		return s.bareSKUsByVariantColumn(ctx, pid)
	}
	stockIDs, err := s.m.ListAllStockIDs(ctx, pid)
	if err != nil {
		return nil, err
	}
	if len(stockIDs) == 0 {
		return out, nil
	}
	byStock, err := s.refs.VariantIDsByWarehouseStocks(ctx, pid, stockIDs)
	if err != nil {
		return nil, err
	}
	facts, err := s.m.ListStockFactsByIDs(ctx, pid, stockIDs)
	if err != nil {
		return nil, err
	}
	for _, f := range facts {
		if f == nil || f.SKUCode == "" {
			continue
		}
		for _, variantID := range byStock[f.ID] {
			if _, seen := out[variantID]; seen {
				continue
			}
			out[variantID] = f.SKUCode
		}
	}
	return out, nil
}

func (s *Service) bareSKUsByVariantColumn(ctx context.Context, projectID string) (map[string]string, error) {
	rows, err := s.m.ListStockRows(ctx, inventorymodel.StockFilter{ProjectID: projectID}, 0, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if r == nil || r.VariantID == "" || r.SKUCode == "" {
			continue
		}
		if _, seen := out[r.VariantID]; seen {
			continue
		}
		out[r.VariantID] = r.SKUCode
	}
	return out, nil
}

func (s *Service) ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error) {
	page, size := pageArgs(req)
	filter := inventorymodel.StockFilter{
		WarehouseID: strings.TrimSpace(req.WarehouseID),
		ProductID:   strings.TrimSpace(req.ProductID),
		VariantID:   strings.TrimSpace(req.VariantID),
		SKUCode:     strings.TrimSpace(req.SKUCode),
		ExternalSKU: strings.TrimSpace(req.ExternalSKU),
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListStockRows(ctx, filter, size, utils.PageOffset(page, size))
	if err != nil {
		return nil, err
	}
	return toStockRespList(rows), nil
}

func (s *Service) toStockResp(ctx context.Context, e *inventorymodel.StockEntity) *inventorydto.StockResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.StockResp{
		ID: e.ID, ProjectID: e.ProjectID, WarehouseID: e.WarehouseID,
		ProductID: inventorymodel.PtrString(e.ProductID), VariantID: inventorymodel.PtrString(e.VariantID),
		SKUCode: e.SKUCode,

		ExternalSKU: e.ExternalSKU,

		TrackQuantity: e.TrackQuantity,
		Quantity:      e.Quantity,

		ReservedQuantity:  e.ReservedQuantity,
		AvailableQuantity: availableOf(e.Quantity, e.ReservedQuantity),

		CostPrice: e.CostPrice,

		Name: e.Name, ImageURL: e.ImageURL, Barcode: e.Barcode, Remark: e.Remark,
		Status: e.Status, FreightPrice: e.FreightPrice,
		CostCurrency: e.CostCurrency, CostBaseCurrency: e.CostBaseCurrency, CostFXRate: e.CostFXRate,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if wh, err := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); err == nil {
		resp.WarehouseCode, resp.WarehouseName = wh.Code, wh.Name
	}
	return resp
}

func toStockRespList(rows []*inventorymodel.StockRow) []*inventorydto.StockResp {
	list := make([]*inventorydto.StockResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, &inventorydto.StockResp{
			ID: r.ID, ProjectID: r.ProjectID, WarehouseID: r.WarehouseID,
			WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
			ProductID: r.ProductID, VariantID: r.VariantID,
			SKUCode: r.SKUCode,

			ExternalSKU: r.ExternalSKU,

			TrackQuantity: r.TrackQuantity,
			Quantity:      r.Quantity,

			ReservedQuantity:  r.ReservedQuantity,
			AvailableQuantity: availableOf(r.Quantity, r.ReservedQuantity),

			CostPrice: r.CostPrice,
			Name:      r.Name, ImageURL: r.ImageURL, Barcode: r.Barcode, Remark: r.Remark,
			Status: r.Status, FreightPrice: r.FreightPrice,
			CostCurrency: r.CostCurrency, CostBaseCurrency: r.CostBaseCurrency, CostFXRate: r.CostFXRate,
			CreatedAt: r.CreatedAt.Format(time.RFC3339), UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
		})
	}
	return list
}

func (s *Service) WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error) {
	if len(productIDs) == 0 || s.refs == nil {
		return []inventorydto.ProductWarehouseStock{}, nil
	}
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stockIDs, err := s.m.ListAllStockIDs(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(stockIDs) == 0 {
		return []inventorydto.ProductWarehouseStock{}, nil
	}
	byStock, err := s.refs.VariantIDsByWarehouseStocks(ctx, projectID, stockIDs)
	if err != nil {
		return nil, err
	}
	if len(byStock) == 0 {
		return []inventorydto.ProductWarehouseStock{}, nil
	}
	owners, err := s.productOfStocks(ctx, projectID, byStock)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(productIDs))
	for _, id := range productIDs {
		wanted[id] = true
	}
	owned := make([]string, 0, len(owners))
	for stockID, productID := range owners {
		if wanted[productID] {
			owned = append(owned, stockID)
		}
	}
	if len(owned) == 0 {
		return []inventorydto.ProductWarehouseStock{}, nil
	}
	facts, err := s.m.StockFactsByStockIDs(ctx, projectID, owned)
	if err != nil {
		return nil, err
	}
	out = make([]inventorydto.ProductWarehouseStock, 0, len(facts))
	for _, f := range facts {
		out = append(out, inventorydto.ProductWarehouseStock{
			ProductID:     owners[f.StockID],
			WarehouseID:   f.WarehouseID,
			WarehouseCode: f.WarehouseCode,
			WarehouseName: f.WarehouseName,
			SKUCode:       f.SKUCode,
			TrackQuantity: f.TrackQuantity,
			Quantity:      f.Quantity,
			CostPrice:     f.CostPrice,
		})
	}
	return out, nil
}

func (s *Service) productOfStocks(ctx context.Context, projectID string, byStock map[string][]string) (map[string]string, error) {
	out := make(map[string]string, len(byStock))
	ids := make([]string, 0, len(byStock))
	for _, variants := range byStock {
		ids = append(ids, variants...)
	}
	if len(ids) == 0 {
		return out, nil
	}
	productOf, err := s.refs.VariantProductIDs(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	for stockID, variants := range byStock {
		for _, v := range variants {
			if pid := productOf[v]; pid != "" {
				out[stockID] = pid
				break
			}
		}
	}
	return out, nil
}

func (s *Service) UpdateStockTracking(ctx context.Context, req *inventorydto.UpdateStockTrackingReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.VariantID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if req.Quantity < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
	}
	if !req.TrackQuantity && req.Quantity != 0 {

		return nil, inventoryenums.Err(inventoryenums.ErrStockUntrackedQuantity)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	warehouseID := strings.TrimSpace(req.WarehouseID)
	variantID := strings.TrimSpace(req.VariantID)

	skuCode := stripWarehousePrefix(strings.TrimSpace(req.SKUCode), "")
	row, err := s.locateStockRow(ctx, projectID, warehouseID, skuCode, variantID)
	if err != nil {
		return nil, mapStockNotFound(err)
	}
	skuCode = row.SKUCode

	adjustTo := func(target int, trackOverride map[stockKey]bool) error {
		_, cerr := s.changeStockTracking(ctx, &inventorydto.ChangeStockReq{
			ProjectID:   projectID,
			WarehouseID: warehouseID,
			Direction:   inventoryenums.DirectionAdjust,
			ReasonCode:  inventoryenums.ReasonManualAdjust,
			SourceType:  inventoryenums.MovementSourceInventoryPage,
			Remark:      "库存页行内编辑：跟踪开关 / 数量",
			Lines: []inventorydto.StockChangeLineReq{{
				WarehouseID: warehouseID,
				VariantID:   inventorymodel.PtrString(row.VariantID),
				ProductID:   inventorymodel.PtrString(row.ProductID),
				SKUCode:     skuCode,
				Quantity:    target,
			}},
		}, trackOverride)
		return cerr
	}

	if req.TrackQuantity {

		if !row.TrackQuantity || row.Quantity != req.Quantity {
			if err = adjustTo(req.Quantity, nil); err != nil {
				return nil, err
			}
		}
		return s.stockRespByID(ctx, projectID, row.ID)
	}

	if row.Quantity != 0 || row.TrackQuantity {
		key := stockKey{skuCode: skuCode, warehouseID: warehouseID}
		if err = adjustTo(0, map[stockKey]bool{key: false}); err != nil {
			return nil, err
		}
	}
	return s.stockRespByID(ctx, projectID, row.ID)
}

func (s *Service) locateStockRow(ctx context.Context, projectID, warehouseID, skuCode, variantID string) (e *inventorymodel.StockEntity, err error) {
	if skuCode != "" {
		return s.m.GetStockByWarehouseSKU(ctx, warehouseID, skuCode, projectID)
	}
	if strings.TrimSpace(variantID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrStockSKURequired)
	}
	return s.m.GetStockByVariantWarehouse(ctx, variantID, warehouseID, projectID)
}

func (s *Service) stockRespByID(ctx context.Context, projectID, id string) (*inventorydto.StockResp, error) {
	e, err := s.m.GetStock(ctx, id, projectID)
	if err != nil {
		return nil, mapStockNotFound(err)
	}
	return s.toStockResp(ctx, e), nil
}

func mapStockNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return inventoryenums.Err(inventoryenums.ErrStockNotFound)
	}
	return err
}

func (s *Service) ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error) {
	if len(refs) == 0 {
		return nil, nil
	}
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	variantIDs := make([]string, 0, len(refs))
	for _, r := range refs {
		if id := strings.TrimSpace(r.VariantID); id != "" {
			variantIDs = append(variantIDs, id)
		}
	}

	cost := make(map[string]*float64, len(refs))
	if s.refs != nil {
		byVariant, rerr := s.refs.WarehouseStockIDsByVariant(ctx, projectID, variantIDs)
		if rerr != nil {
			return nil, rerr
		}
		stockIDs := make([]string, 0, len(byVariant)*2)
		for _, ids := range byVariant {
			stockIDs = append(stockIDs, ids...)
		}
		facts, ferr := s.m.StockFactsByStockIDs(ctx, projectID, stockIDs)
		if ferr != nil {
			return nil, ferr
		}
		byStock := make(map[string]*inventorycontract.StockFact, len(facts))
		for _, f := range facts {
			byStock[f.StockID] = f
		}

		for variantID, ids := range byVariant {
			for _, id := range ids {
				f := byStock[id]
				if f == nil {
					continue
				}
				cost[variantID+"|"+f.WarehouseID] = f.CostPrice
			}
		}
	}

	resolved := make(map[string]*inventorymodel.WarehouseEntity, len(refs))
	out = make([]inventorydto.VariantWarehouseCost, 0, len(refs))
	for _, r := range refs {
		variantID := strings.TrimSpace(r.VariantID)
		if variantID == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
		}
		key := strings.TrimSpace(r.WarehouseID)
		wh, ok := resolved[key]
		if !ok {
			var werr error
			wh, werr = s.resolveWarehouse(ctx, projectID, key)
			if werr != nil {
				return nil, werr
			}
			resolved[key] = wh
		}
		out = append(out, inventorydto.VariantWarehouseCost{
			VariantID:   variantID,
			WarehouseID: wh.ID,
			CostPrice:   cost[variantID+"|"+wh.ID],
		})
	}
	return out, nil
}

func (s *Service) VariantHasStockMovement(ctx context.Context, projectID, variantID string) (bool, error) {
	return s.m.VariantHasStockMovement(ctx, projectID, variantID)
}

const skuSeparator = "_"

func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

func normalizeStockSKU(code, warehouseCode string) (bare string, err error) {
	bare = stripWarehousePrefix(code, warehouseCode)
	if bare == "" {
		return "", inventoryenums.Err(inventoryenums.ErrStockSKURequired)
	}
	return bare, nil
}

var _ inventorycontract.ProductStockPort = (*Service)(nil)

func (s *Service) StockFactsByStockIDs(ctx context.Context, projectID string, stockIDs []string) (facts []*inventorycontract.StockFact, err error) {
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.m.StockFactsByStockIDs(ctx, pid, stockIDs)
}

func (s *Service) NormalizeExternalSKU(raw string) (code string, err error) {
	return NormalizeExternalSKU(raw)
}

func (s *Service) ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	return &productcontract.WarehouseRef{
		ID: wh.ID, ProjectID: wh.ProjectID, Code: wh.Code, Name: wh.Name,
	}, nil
}

func (s *Service) WarehouseStockIDBySKU(ctx context.Context, projectID, warehouseID, skuCode string) (stockID string, err error) {
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return "", err
	}
	e, ferr := s.m.FindStockByWarehouseSKU(ctx, projectID, strings.TrimSpace(warehouseID), strings.TrimSpace(skuCode))
	if errors.Is(ferr, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if ferr != nil {
		return "", ferr
	}
	return e.ID, nil
}

func (s *Service) EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error) {

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		var xerr error
		stockID, xerr = s.EnsureVariantStockTx(ctx, tx, ref, productID, variantID, skuCode, quantity)
		return xerr
	})
	return stockID, err
}

func (s *Service) EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return "", inventoryenums.Err(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return "", inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
	}
	e, err := s.ensureStockRowTx(ctx, tx, ref.ProjectID, ref.ID, productID, variantID, skuCode, quantity)
	if err != nil {
		return "", err
	}
	s.attachStockRef(ctx, tx, ref, variantID, e)
	return e.ID, nil
}

func (s *Service) EnsureVariantStockWithExternal(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (stockID string, err error) {

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		var xerr error
		stockID, xerr = s.EnsureVariantStockWithExternalTx(ctx, tx, ref, productID, variantID, skuCode, externalSKU, quantity)
		return xerr
	})
	return stockID, err
}

func (s *Service) EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (stockID string, err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return "", inventoryenums.Err(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return "", inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
	}
	code, err := NormalizeExternalSKU(externalSKU)
	if err != nil {
		return "", err
	}
	e, err := s.ensureStockRowWithExternalTx(ctx, tx, ref.ProjectID, ref.ID, productID, variantID, skuCode, code, quantity)
	if err != nil {
		return "", err
	}
	s.attachStockRef(ctx, tx, ref, variantID, e)
	return e.ID, nil
}

func (s *Service) attachStockRef(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, variantID string, e *inventorymodel.StockEntity) {
	if s.refs == nil || e == nil || strings.TrimSpace(variantID) == "" {
		return
	}
	err := s.refs.AttachWarehouseStockTx(ctx, tx, e.ProjectID, variantID, productcontract.WarehouseStockRef{
		StockID:       e.ID,
		WarehouseID:   e.WarehouseID,
		WarehouseCode: refCodeOf(ref),
		SKUCode:       e.SKUCode,
	})
	if err != nil {
		logger.Scene("inventory").With("stockId", e.ID).With("variantId", variantID).
			With("err", err).Warn("登记变体货行引用失败（可用量 / 成本投影会缺这一行）")
	}
}

func refCodeOf(ref *productcontract.WarehouseRef) string {
	if ref == nil {
		return ""
	}
	return ref.Code
}

func (s *Service) AvailableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	out = make(map[string]int, len(variantIDs))
	if len(variantIDs) == 0 || s.refs == nil {
		return out, nil
	}
	byVariant, err := s.refs.WarehouseStockIDsByVariant(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	stockIDs := make([]string, 0, len(byVariant)*2)
	for _, ids := range byVariant {
		stockIDs = append(stockIDs, ids...)
	}
	facts, err := s.m.StockFactsByStockIDs(ctx, projectID, stockIDs)
	if err != nil {
		return nil, err
	}
	byStock := make(map[string]*inventorycontract.StockFact, len(facts))
	for _, f := range facts {
		byStock[f.StockID] = f
	}
	for variantID, ids := range byVariant {
		if total, known := finiteStockAvailability(ids, byStock); known {
			out[variantID] = total
		}
	}
	return out, nil
}

func finiteStockAvailability(ids []string, byStock map[string]*inventorycontract.StockFact) (int, bool) {
	if len(ids) == 0 {
		return 0, false
	}
	total := 0
	for _, id := range ids {
		f := byStock[id]
		if f == nil || !f.TrackQuantity {
			return 0, false
		}
		total += availableOf(f.Quantity, f.ReservedQuantity)
	}
	return total, true
}

const (
	maxExternalSKULen = 128

	defaultWarehouseSKUSize = 50
	maxWarehouseSKUSize     = 200
)

func NormalizeExternalSKU(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return "", nil
	}
	if utf8.RuneCountInString(code) > maxExternalSKULen {
		return "", inventoryenums.Err(inventoryenums.ErrExternalSKUInvalid)
	}
	for _, r := range code {

		if r < 0x20 || r == 0x7f {
			return "", inventoryenums.Err(inventoryenums.ErrExternalSKUInvalid)
		}
	}
	return code, nil
}

func (s *Service) ListWarehouseSKUs(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (list []*inventorydto.WarehouseSKUResp, err error) {
	page, size := warehouseSKUPageArgs(req)
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListWarehouseSKUs(ctx, inventorymodel.WarehouseSKUFilter{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(listWarehouseSKUWarehouseID(req)),
		Keyword:     strings.TrimSpace(listWarehouseSKUKeyword(req)),
		Status:      strings.TrimSpace(listWarehouseSKUStatus(req)),
	}, size, utils.PageOffset(page, size))
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.WarehouseSKUResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toWarehouseSKUResp(r))
	}
	return list, nil
}

func (s *Service) LocateStockRow(ctx context.Context, projectID, variantID, warehouseID string) (string, error) {
	e, err := s.m.GetStockByVariantWarehouse(ctx, variantID, warehouseID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if e == nil {
		return "", nil
	}
	return e.ID, nil
}

func (s *Service) GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.SKUCode) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseSKURequired)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	e, err := s.lookupWarehouseSKU(ctx, projectID, strings.TrimSpace(req.WarehouseID), strings.TrimSpace(req.SKUCode))
	if err != nil {
		return nil, err
	}
	return s.toWarehouseSKURespFromEntity(ctx, e), nil
}

func (s *Service) BindExternalSKU(ctx context.Context, req *inventorydto.BindExternalSKUReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.VariantID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	warehouseID := strings.TrimSpace(req.WarehouseID)
	variantID := strings.TrimSpace(req.VariantID)
	code, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	row, err := s.m.GetStockByVariantWarehouse(ctx, variantID, warehouseID, projectID)
	if err != nil {
		return nil, mapStockNotFound(err)
	}
	if err = s.assertExternalSKUProductScope(ctx, projectID, warehouseID, code, inventorymodel.PtrString(row.ProductID)); err != nil {
		return nil, err
	}
	affected, err := s.m.SetExternalSKUByVariantWarehouse(ctx, projectID, warehouseID, variantID, code)
	if err != nil {
		return nil, err
	}
	if affected == 0 {

		return nil, inventoryenums.Err(inventoryenums.ErrStockNotFound)
	}
	row.ExternalSKU = code
	return s.toStockResp(ctx, row), nil
}

func (s *Service) CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error) {
	code, err := NormalizeExternalSKU(externalSKU)
	if err != nil {
		return err
	}
	return s.assertExternalSKUProductScope(ctx, projectID, warehouseID, code, productID)
}

func (s *Service) CheckStockSKUCodeFree(ctx context.Context, projectID, warehouseID, skuCode string) (err error) {
	exists, err := s.m.StockSKUCodeExists(ctx, projectID, warehouseID, strings.TrimSpace(skuCode), "")
	if err != nil {
		return err
	}
	if exists {
		return inventoryenums.Err(inventoryenums.ErrWarehouseSKUCodeTaken)
	}
	return nil
}

func (s *Service) lookupWarehouseSKU(ctx context.Context, projectID, warehouseID, skuCode string) (e *inventorymodel.StockEntity, err error) {
	e, err = s.m.FindStockByWarehouseSKU(ctx, projectID, warehouseID, skuCode)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, inventoryenums.Err(inventoryenums.ErrWarehouseSKUNotFound)
		}
		return nil, err
	}
	return e, nil
}

func (s *Service) assertExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error) {
	if externalSKU == "" {
		return nil
	}
	owner, err := s.m.ExternalSKUProductConflict(ctx, projectID, warehouseID, externalSKU, productID)
	if err != nil {
		return err
	}
	if owner != "" {

		return inventoryenums.ErrDetail(inventoryenums.ErrExternalSKUProductConflict,
			i18n.ErrorDetail(inventoryenums.DetailExternalSKUOwner, "name", owner))
	}
	return nil
}

func toWarehouseSKUResp(r *inventorymodel.WarehouseSKURow) *inventorydto.WarehouseSKUResp {
	return &inventorydto.WarehouseSKUResp{
		WarehouseID: r.WarehouseID, WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
		IsDefault: r.IsDefault, SKUCode: r.SKUCode, ExternalSKU: r.ExternalSKU,
		ProductID: inventorymodel.PtrString(r.ProductID), VariantID: inventorymodel.PtrString(r.VariantID),
		HasVariant: r.HasVariant,

		Quantity: r.Quantity, ReservedQuantity: r.ReservedQuantity,
		AvailableQuantity: availableOf(r.Quantity, r.ReservedQuantity),
		TrackQuantity:     r.TrackQuantity,

		CostPrice: r.CostPrice, FreightPrice: r.FreightPrice,
		CostCurrency: r.CostCurrency, CostBaseCurrency: r.CostBaseCurrency, CostFXRate: r.CostFXRate,
		Name: r.Name, ImageURL: r.ImageURL, Barcode: r.Barcode, Remark: r.Remark, Status: r.Status,
		CreatedAt: r.CreatedAt.Format(time.RFC3339), UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Service) toWarehouseSKURespFromEntity(ctx context.Context, e *inventorymodel.StockEntity) *inventorydto.WarehouseSKUResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.WarehouseSKUResp{
		ID:          e.ID,
		WarehouseID: e.WarehouseID, SKUCode: e.SKUCode, ExternalSKU: e.ExternalSKU,
		ProductID: inventorymodel.PtrString(e.ProductID), VariantID: inventorymodel.PtrString(e.VariantID),
		HasVariant:        e.VariantID != nil,
		Quantity:          e.Quantity,
		ReservedQuantity:  e.ReservedQuantity,
		AvailableQuantity: availableOf(e.Quantity, e.ReservedQuantity),
		TrackQuantity:     e.TrackQuantity,
		CostPrice:         e.CostPrice, FreightPrice: e.FreightPrice,
		CostCurrency: e.CostCurrency, CostBaseCurrency: e.CostBaseCurrency, CostFXRate: e.CostFXRate,
		Name: e.Name, ImageURL: e.ImageURL, Barcode: e.Barcode, Remark: e.Remark, Status: e.Status,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if wh, werr := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); werr == nil {
		resp.WarehouseCode, resp.WarehouseName, resp.IsDefault = wh.Code, wh.Name, wh.IsDefault
	}
	return resp
}

func warehouseSKUPageArgs(req *inventorydto.ListWarehouseSKUReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultWarehouseSKUSize, maxWarehouseSKUSize)
	return paging.Page, paging.Size
}

func listWarehouseSKUProjectID(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

func listWarehouseSKUWarehouseID(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.WarehouseID
}

func listWarehouseSKUKeyword(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.Keyword
}

func listWarehouseSKUStatus(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.Status
}

func (s *Service) VariantDeletionFactsTx(ctx context.Context, tx *gorm.DB, projectID string, variantIDs, stockIDs []string) (*inventorycontract.VariantDeletionFacts, error) {
	return s.m.VariantDeletionFactsTx(ctx, tx, projectID, variantIDs, stockIDs)
}
