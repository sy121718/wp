package inventoryservice

import (
	"cmp"
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

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

const (
	maxBatchLines = 200

	movementDefaultPageSize = 20
	movementMaxPageSize     = 200
)

type stockKey struct {
	warehouseID string
	skuCode     string
}

type changeItem struct {
	key       stockKey
	variantID string
	direction string

	quantity int

	projectID string
	productID string
	skuCode   string
	reason    *inventorymodel.ReasonEntity

	cost       *float64
	warehouse  *inventorymodel.WarehouseEntity
	sourceType string
	sourceRef  string
	remark     string
	operatorID string
}

type changeOutcome struct {
	batchID   string
	movements []*inventorymodel.MovementEntity

	variants map[string]string

	warehouses  map[string]*inventorymodel.WarehouseEntity
	reasonNames map[string]string
}

type stockPolicy int

const (
	stockPolicyExplicit stockPolicy = iota

	stockPolicyRestore
)

type changeMeta struct {
	reason     *inventorymodel.ReasonEntity
	sourceType string
	sourceRef  string
	remark     string
	operatorID string
}

func (s *Service) ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error) {
	return s.changeStockTracking(ctx, req, nil)
}

func (s *Service) ChangeStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (err error) {
	_, _, _, err = s.changeStockTrackingTx(ctx, tx, req, nil)
	return err
}

func (s *Service) ChangeStockTxWithBatch(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (batchID string, err error) {
	out, _, _, cerr := s.changeStockTrackingTx(ctx, tx, req, nil)
	if cerr != nil {
		return "", cerr
	}
	if out == nil {
		return "", nil
	}
	return out.batchID, nil
}

var _ inventorycontract.BatchStockWriter = (*Service)(nil)

func (s *Service) RestockStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error) {
	out, projectID, direction, err := s.changeStockTrackingPolicyTx(ctx, nil, req, nil, stockPolicyRestore)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, direction, out), nil
}

func (s *Service) RestockStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (err error) {
	_, _, _, err = s.changeStockTrackingPolicyTx(ctx, tx, req, nil, stockPolicyRestore)
	return err
}

func (s *Service) changeStockTracking(ctx context.Context, req *inventorydto.ChangeStockReq,
	trackOverride map[stockKey]bool) (res *inventorydto.StockChangeResp, err error) {
	out, projectID, direction, err := s.changeStockTrackingTx(ctx, nil, req, trackOverride)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, direction, out), nil
}

func (s *Service) changeStockTrackingTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq,
	trackOverride map[stockKey]bool) (out *changeOutcome, projectID, direction string, err error) {
	return s.changeStockTrackingPolicyTx(ctx, tx, req, trackOverride, stockPolicyExplicit)
}

func (s *Service) changeStockTrackingPolicyTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq,
	trackOverride map[stockKey]bool, policy stockPolicy) (out *changeOutcome, projectID, direction string, err error) {
	if req == nil {
		return nil, "", "", inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if direction, err = normalizeDirection(req.Direction); err != nil {
		return nil, "", "", err
	}
	if len(req.Lines) == 0 {
		return nil, "", "", inventoryenums.Err(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, "", "", inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, "", "", err
	}

	reason, err := s.resolveReason(ctx, projectID, req.ReasonCode, direction)
	if err != nil {
		return nil, "", "", err
	}
	meta := changeMeta{reason: reason, sourceType: req.SourceType, sourceRef: req.SourceRef,
		remark: req.Remark, operatorID: req.OperatorID}
	items, err := s.buildChangeItems(ctx, projectID, direction, req.WarehouseID, req.Lines, meta)
	if err != nil {
		return nil, "", "", err
	}
	if out, err = s.applyStockChangesOn(ctx, tx, projectID, items, trackOverride, policy); err != nil {
		return nil, "", "", err
	}
	return out, projectID, direction, nil
}

func (s *Service) DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error) {
	out, projectID, err := s.deductStockTx(ctx, nil, req)
	if err != nil {
		return nil, err
	}
	return s.finishChange(ctx, projectID, inventoryenums.DirectionOut, out), nil
}

func (s *Service) DeductStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.DeductStockReq) (err error) {
	_, _, err = s.deductStockTx(ctx, tx, req)
	return err
}

func (s *Service) deductStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.DeductStockReq) (
	out *changeOutcome, projectID string, err error) {
	if req == nil {
		return nil, "", inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if len(req.Lines) == 0 {
		return nil, "", inventoryenums.Err(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, "", inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, "", err
	}
	reason, err := s.resolveReason(ctx, projectID, req.ReasonCode, inventoryenums.DirectionOut)
	if err != nil {
		return nil, "", err
	}
	meta := changeMeta{reason: reason, sourceType: req.SourceType, sourceRef: req.SourceRef,
		remark: req.Remark, operatorID: req.OperatorID}

	lines, aerr := s.assignDeductWarehouses(ctx, projectID, inventoryenums.DirectionOut, req.WarehouseID, req.Lines)
	if aerr != nil {
		return nil, "", aerr
	}
	items, err := s.buildChangeItems(ctx, projectID, inventoryenums.DirectionOut, req.WarehouseID, lines, meta)
	if err != nil {
		return nil, "", err
	}
	if out, err = s.applyStockChangesOn(ctx, tx, projectID, items, nil, stockPolicyExplicit); err != nil {
		return nil, "", err
	}
	return out, projectID, nil
}

func (s *Service) ListMovements(ctx context.Context, req *inventorydto.ListMovementReq) (list []*inventorydto.MovementResp, err error) {
	filter, err := s.movementFilter(ctx, req)
	if err != nil {
		return nil, err
	}
	page, size := movementPageArgs(req)
	rows, err := s.m.ListMovementRows(ctx, filter, size, utils.PageOffset(page, size))
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.MovementResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toMovementResp(r))
	}
	return list, nil
}

func (s *Service) CountMovements(ctx context.Context, req *inventorydto.ListMovementReq) (n int64, err error) {
	filter, err := s.movementFilter(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.CountMovements(ctx, filter)
}

func (s *Service) movementFilter(ctx context.Context, req *inventorydto.ListMovementReq) (filter inventorymodel.MovementFilter, err error) {
	if req == nil {
		return filter, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	filter = inventorymodel.MovementFilter{
		WarehouseID: strings.TrimSpace(req.WarehouseID),
		ProductID:   strings.TrimSpace(req.ProductID),
		VariantID:   strings.TrimSpace(req.VariantID),
		SKUCode:     strings.TrimSpace(req.SKUCode),
		ReasonCode:  strings.TrimSpace(req.ReasonCode),
		SourceType:  strings.TrimSpace(req.SourceType),
		SourceRef:   strings.TrimSpace(req.SourceRef),
		BatchID:     strings.TrimSpace(req.BatchID),
	}
	if filter.Direction, err = normalizeDirectionOrEmpty(req.Direction); err != nil {
		return filter, err
	}

	if filter.TimeFrom, err = parseMovementTime(req.TimeFrom, false); err != nil {
		return filter, err
	}
	if filter.TimeTo, err = parseMovementTime(req.TimeTo, true); err != nil {
		return filter, err
	}
	if filter.TimeFrom != nil && filter.TimeTo != nil && filter.TimeTo.Before(*filter.TimeFrom) {
		return filter, inventoryenums.Err(inventoryenums.ErrMovementTimeRangeInvalid)
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return filter, err
	}
	return filter, nil
}

func (s *Service) buildChangeItems(ctx context.Context, projectID, direction, defaultWarehouseID string,
	lines []inventorydto.StockChangeLineReq, meta changeMeta) (items []changeItem, err error) {
	items = make([]changeItem, 0, len(lines))
	for i := range lines {
		line := lines[i]
		variantID := strings.TrimSpace(line.VariantID)
		if variantID == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
		}
		quantity := line.Quantity
		switch direction {
		case inventoryenums.DirectionIn, inventoryenums.DirectionOut:
			if quantity <= 0 {
				return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
			}
		case inventoryenums.DirectionAdjust:
			if quantity < 0 {
				return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
			}
		}

		want := strings.TrimSpace(line.WarehouseID)
		if want == "" {
			want = defaultWarehouseID
		}
		wh, werr := s.resolveWarehouse(ctx, projectID, want)
		if werr != nil {
			return nil, werr
		}

		key, serr := s.resolveLineStockKey(ctx, projectID, wh, variantID, line.SKUCode)
		if serr != nil {
			return nil, serr
		}

		cost, cerr := normalizeExplicitCost(line.CostPrice)
		if cerr != nil {
			return nil, cerr
		}
		items = append(items, changeItem{
			key:        key,
			variantID:  variantID,
			direction:  direction,
			quantity:   quantity,
			cost:       cost,
			projectID:  wh.ProjectID,
			productID:  strings.TrimSpace(line.ProductID),
			skuCode:    key.skuCode,
			reason:     meta.reason,
			warehouse:  wh,
			sourceType: meta.sourceType,
			sourceRef:  meta.sourceRef,
			remark:     meta.remark,
			operatorID: meta.operatorID,
		})
	}
	return items, nil
}

func (s *Service) resolveLineStockKey(ctx context.Context, projectID string, wh *inventorymodel.WarehouseEntity,
	variantID, raw string) (key stockKey, err error) {
	bare, berr := normalizeStockSKU(strings.TrimSpace(raw), wh.Code)
	if berr != nil {
		bare = ""
	}
	if bare != "" {
		row, gerr := s.m.GetStockByWarehouseSKU(ctx, wh.ID, bare, projectID)
		if gerr == nil && row != nil {
			return stockKey{warehouseID: wh.ID, skuCode: row.SKUCode}, nil
		}
		if gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return stockKey{}, gerr
		}
	}
	row, gerr := s.m.GetStockByVariantWarehouse(ctx, variantID, wh.ID, projectID)
	if gerr == nil && row != nil {
		return stockKey{warehouseID: wh.ID, skuCode: row.SKUCode}, nil
	}
	if gerr != nil && !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return stockKey{}, gerr
	}

	if bare == "" && variantID != "" && s.refs != nil {
		if codes, cerr := s.refs.VariantSKUCodes(ctx, projectID, []string{variantID}); cerr == nil {
			bare, _ = normalizeStockSKU(strings.TrimSpace(codes[variantID]), wh.Code)
		}
	}
	if bare == "" {
		return stockKey{}, inventoryenums.Err(inventoryenums.ErrStockSKURequired)
	}
	return stockKey{warehouseID: wh.ID, skuCode: bare}, nil
}

func (s *Service) applyStockChangesOn(ctx context.Context, tx *gorm.DB, projectID string, items []changeItem,
	trackOverride map[stockKey]bool, policy stockPolicy) (out *changeOutcome, err error) {
	if tx != nil {
		return s.applyStockChangesTx(ctx, tx, projectID, items, trackOverride, policy)
	}
	err = s.m.Transaction(ctx, func(t *gorm.DB) error {
		var e error
		out, e = s.applyStockChangesTx(ctx, t, projectID, items, trackOverride, policy)
		return e
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) applyStockChangesTx(ctx context.Context, tx *gorm.DB, projectID string, items []changeItem,
	trackOverride map[stockKey]bool, policy stockPolicy) (out *changeOutcome, err error) {
	if len(items) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesRequired)
	}
	now := time.Now().UTC()
	out = &changeOutcome{
		batchID:     uuid.NewString(),
		variants:    map[string]string{},
		warehouses:  map[string]*inventorymodel.WarehouseEntity{},
		reasonNames: map[string]string{},
	}
	for _, it := range items {
		if it.warehouse != nil {
			out.warehouses[it.key.warehouseID] = it.warehouse
		}
		if it.reason != nil {
			out.reasonNames[it.reason.Code] = it.reason.Name
		}
	}
	keys := uniqueSortedKeys(items)
	variantIDs := distinctVariantIDs(items)

	costs := explicitCosts(items)

	err = func() error {

		existing, lerr := s.m.ListStocksByVariantsTx(ctx, tx, variantIDs, projectID)
		if lerr != nil {
			return lerr
		}
		byKey := make(map[stockKey]*inventorymodel.StockEntity, len(existing))
		meta := make(map[string]*inventorymodel.StockEntity, len(existing))
		for _, e := range existing {
			byKey[stockKey{warehouseID: e.WarehouseID, skuCode: e.SKUCode}] = e
			variantID := inventorymodel.PtrString(e.VariantID)
			if _, ok := meta[variantID]; !ok {
				meta[variantID] = e
			}
		}

		pending := make([]*inventorymodel.StockEntity, 0, len(keys))
		for _, k := range keys {
			if _, ok := byKey[k]; ok {
				continue
			}
			item := firstItemOf(items, k)

			productID, skuCode := item.productID, k.skuCode
			if src, ok := meta[item.variantID]; ok && productID == "" {
				productID = inventorymodel.PtrString(src.ProductID)
			}
			if productID == "" {
				return inventoryenums.Err(inventoryenums.ErrStockProductRequired)
			}
			pending = append(pending, &inventorymodel.StockEntity{
				ID: uuid.NewString(), ProjectID: orString(item.projectID, projectID),

				WarehouseID: k.warehouseID,
				ProductID:   inventorymodel.StringPtr(productID),
				VariantID:   inventorymodel.StringPtr(item.variantID),
				SKUCode:     skuCode,

				TrackQuantity: false, Quantity: 0,
				Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
			})
		}
		if err := s.m.EnsureStocksTx(ctx, tx, pending); err != nil {
			return err
		}

		locked := make(map[stockKey]*inventorymodel.StockEntity, len(keys))
		for _, k := range keys {
			e, lerr := s.m.LockStockBySKUTx(ctx, tx, projectID, k.warehouseID, k.skuCode)
			if lerr != nil {
				return lerr
			}
			locked[k] = e
		}

		if s.refs != nil {
			for _, k := range keys {
				e := locked[k]
				if e == nil || e.ID == "" {
					continue
				}
				for _, it := range items {
					if it.key != k || strings.TrimSpace(it.variantID) == "" {
						continue
					}
					ref := productcontract.WarehouseStockRef{
						StockID:     e.ID,
						WarehouseID: e.WarehouseID,
						SKUCode:     e.SKUCode,
					}
					if it.warehouse != nil {
						ref.WarehouseCode = it.warehouse.Code
					}
					if aerr := s.refs.AttachWarehouseStockTx(ctx, tx, projectID, it.variantID, ref); aerr != nil {
						logger.Scene("inventory").With("stockId", e.ID).With("variantId", it.variantID).
							With("err", aerr).Warn("登记变体货行引用失败（可用量 / 成本投影会缺这一行）")
					}
				}
			}
		}

		changed := make(map[stockKey]bool, len(keys))
		for _, it := range sortItems(items) {
			e := locked[it.key]
			if !e.TrackQuantity && it.direction == inventoryenums.DirectionOut {
				continue
			}

			if !e.TrackQuantity && policy == stockPolicyRestore {
				continue
			}
			before := e.Quantity
			var delta int
			switch it.direction {
			case inventoryenums.DirectionIn:
				delta = it.quantity
			case inventoryenums.DirectionOut:
				delta = -it.quantity
			case inventoryenums.DirectionAdjust:
				delta = it.quantity - before
			}
			after := before + delta
			if after < 0 {

				return inventoryenums.Err(inventoryenums.ErrStockInsufficient)
			}
			switched := false
			if !e.TrackQuantity {
				e.TrackQuantity = true
				switched = true
			}
			if delta == 0 {

				if switched {
					changed[it.key] = true
				}
				continue
			}

			if isShipOut(it) {
				if derr := s.deductOnShip(ctx, tx, e.ID, abs(delta), now); derr != nil {
					return derr
				}
				e.Quantity = after
				out.variants[it.variantID] = e.SKUCode
				out.movements = append(out.movements, movementOf(it, e, before, after, delta, out.batchID, now))
				continue
			}
			e.Quantity = after
			changed[it.key] = true
			out.variants[it.variantID] = e.SKUCode
			out.movements = append(out.movements, movementOf(it, e, before, after, delta, out.batchID, now))
		}
		for _, k := range keys {
			e := locked[k]

			if target, ok := trackOverride[k]; ok && target != e.TrackQuantity {
				e.TrackQuantity = target
				changed[k] = true
			}
			if !changed[k] {
				continue
			}

			if !e.TrackQuantity && e.Quantity != 0 {
				return inventoryenums.Err(inventoryenums.ErrStockUntrackedQuantity)
			}
			if err := s.m.UpdateStockQuantityAndTrackingTx(ctx, tx, e.ID,
				e.Quantity, e.TrackQuantity, now); err != nil {
				return err
			}
		}

		for _, k := range keys {
			cost, ok := costs[k]
			if !ok {
				continue
			}
			if err := s.m.UpdateStockCostTx(ctx, tx, locked[k].ID, cost, now); err != nil {
				return err
			}
		}
		return s.m.CreateMovementsTx(ctx, tx, out.movements)
	}()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) finishChange(ctx context.Context, projectID, direction string, out *changeOutcome) *inventorydto.StockChangeResp {
	resp := &inventorydto.StockChangeResp{
		BatchID:       out.batchID,
		Direction:     direction,
		Movements:     make([]*inventorydto.MovementResp, 0, len(out.movements)),
		CacheTotals:   map[string]int{},
		CacheSynced:   true,
		CacheFailures: []string{},
	}
	for _, m := range out.movements {
		item := toMovementRespFromEntity(m)
		item.ReasonName = out.reasonNames[m.ReasonCode]
		if wh := out.warehouses[m.WarehouseID]; wh != nil {
			item.WarehouseCode, item.WarehouseName = wh.Code, wh.Name
		}
		resp.Movements = append(resp.Movements, item)
	}
	resp.CacheSynced = len(resp.CacheFailures) == 0
	return resp
}

func movementOf(it changeItem, e *inventorymodel.StockEntity, before, after, delta int, batchID string, now time.Time) *inventorymodel.MovementEntity {
	m := &inventorymodel.MovementEntity{
		ID: utils.NewTimeOrderedID(), ProjectID: e.ProjectID, WarehouseID: e.WarehouseID,
		ProductID: e.ProductID, VariantID: e.VariantID,
		SKUCode:   e.SKUCode,
		Direction: it.direction, Quantity: abs(delta), Delta: delta,
		QuantityBefore: before, QuantityAfter: after,

		StockID: &e.ID,

		StockName:     e.Name,
		StockImageURL: e.ImageURL,
		SourceType:    it.sourceType, SourceRef: it.sourceRef,
		Remark: it.remark, OperatorID: it.operatorID,
		BatchID: batchID, CreatedAt: now,
	}
	if it.reason != nil {
		code, id := it.reason.Code, it.reason.ID
		m.ReasonCode, m.ReasonID = code, &id
	}

	switch {
	case it.cost != nil:
		value := *it.cost
		m.UnitCost = &value
	case e.CostPrice != nil:
		value := *e.CostPrice
		m.UnitCost = &value
	}
	return m
}

func uniqueSortedKeys(items []changeItem) []stockKey {
	seen := make(map[stockKey]bool, len(items))
	keys := make([]stockKey, 0, len(items))
	for _, it := range items {
		if seen[it.key] {
			continue
		}
		seen[it.key] = true
		keys = append(keys, it.key)
	}
	slices.SortFunc(keys, func(a, b stockKey) int {
		return cmp.Or(cmp.Compare(a.warehouseID, b.warehouseID), cmp.Compare(a.skuCode, b.skuCode))
	})
	return keys
}

func sortItems(items []changeItem) []changeItem {
	out := make([]changeItem, len(items))
	copy(out, items)
	slices.SortStableFunc(out, func(a, b changeItem) int {
		return cmp.Or(cmp.Compare(a.key.warehouseID, b.key.warehouseID), cmp.Compare(a.key.skuCode, b.key.skuCode))
	})
	return out
}

func distinctVariantIDs(items []changeItem) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if seen[it.variantID] {
			continue
		}
		seen[it.variantID] = true
		out = append(out, it.variantID)
	}
	return out
}

func firstItemOf(items []changeItem, k stockKey) changeItem {
	for _, it := range items {
		if it.key == k {
			return it
		}
	}
	return changeItem{}
}

func normalizeExplicitCost(raw *float64) (out *float64, err error) {
	if raw == nil {
		return nil, nil
	}
	value := *raw
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockCostInvalid)
	}
	return &value, nil
}

func explicitCosts(items []changeItem) map[stockKey]float64 {
	out := make(map[stockKey]float64)
	for _, it := range items {
		if it.cost != nil {
			out[it.key] = *it.cost
		}
	}
	return out
}

func normalizeDirection(direction string) (out string, err error) {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case inventoryenums.DirectionIn:
		return inventoryenums.DirectionIn, nil
	case inventoryenums.DirectionOut:
		return inventoryenums.DirectionOut, nil
	case inventoryenums.DirectionAdjust:
		return inventoryenums.DirectionAdjust, nil
	default:
		return "", inventoryenums.Err(inventoryenums.ErrStockDirectionInvalid)
	}
}

func normalizeDirectionOrEmpty(direction string) (out string, err error) {
	if strings.TrimSpace(direction) == "" {
		return "", nil
	}
	return normalizeDirection(direction)
}

func movementPageArgs(req *inventorydto.ListMovementReq) (page, size int) {
	page, size = 1, movementDefaultPageSize
	if req == nil {
		return page, size
	}
	if req.Page > 0 {
		page = req.Page
	}
	if req.Size > 0 {
		size = req.Size
		if size > movementMaxPageSize {
			size = movementMaxPageSize
		}
	}
	return page, size
}

func parseMovementTime(raw string, endOfDay bool) (out *time.Time, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{utils.LayoutDay, utils.LayoutSecond, "2006-01-02T15:04", "2006-01-02T15:04:05"} {
		t, terr := time.ParseInLocation(layout, value, time.Local)
		if terr != nil {
			continue
		}
		if endOfDay && layout == utils.LayoutDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return &t, nil
	}
	return nil, inventoryenums.Err(inventoryenums.ErrMovementTimeRangeInvalid)
}

func isShipOut(it changeItem) bool {
	return it.direction == inventoryenums.DirectionOut &&
		it.sourceType == inventoryenums.MovementSourceOrderShipment
}

func (s *Service) deductOnShip(ctx context.Context, tx *gorm.DB, stockID string, quantity int, at time.Time) error {
	affected, err := s.m.DeductStockOnShipTx(ctx, tx, stockID, quantity, at)
	if err != nil {
		return err
	}
	if affected == 0 {
		return inventoryenums.Err(inventoryenums.ErrStockInsufficient)
	}
	return nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func orString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func toMovementResp(r *inventorymodel.MovementRow) *inventorydto.MovementResp {
	resp := &inventorydto.MovementResp{
		ID: r.ID, ProjectID: r.ProjectID, WarehouseID: r.WarehouseID,
		WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
		ProductID: inventorymodel.PtrString(r.ProductID), VariantID: inventorymodel.PtrString(r.VariantID), SKUCode: r.SKUCode,
		Direction: r.Direction, Quantity: r.Quantity, Delta: r.Delta,
		QuantityBefore: r.QuantityBefore, QuantityAfter: r.QuantityAfter,
		ReasonCode: r.ReasonCode, ReasonName: r.ReasonName,
		StockID:   inventorymodel.PtrString(r.StockID),
		StockName: r.StockName, StockImageURL: r.StockImageURL,
		SourceType: r.SourceType, SourceRef: r.SourceRef,
		Remark: r.Remark, OperatorID: r.OperatorID, BatchID: r.BatchID,

		UnitCost:  r.UnitCost,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
	if r.ReasonID != nil {
		resp.ReasonID = strconv.FormatInt(*r.ReasonID, 10)
	}
	return resp
}

func toMovementRespFromEntity(m *inventorymodel.MovementEntity) *inventorydto.MovementResp {
	resp := &inventorydto.MovementResp{
		ID: m.ID, ProjectID: m.ProjectID, WarehouseID: m.WarehouseID,
		ProductID: inventorymodel.PtrString(m.ProductID), VariantID: inventorymodel.PtrString(m.VariantID), SKUCode: m.SKUCode,
		Direction: m.Direction, Quantity: m.Quantity, Delta: m.Delta,
		QuantityBefore: m.QuantityBefore, QuantityAfter: m.QuantityAfter,
		ReasonCode: m.ReasonCode,
		StockID:    inventorymodel.PtrString(m.StockID),
		StockName:  m.StockName, StockImageURL: m.StockImageURL,
		SourceType: m.SourceType, SourceRef: m.SourceRef,
		Remark: m.Remark, OperatorID: m.OperatorID, BatchID: m.BatchID,
		UnitCost:  m.UnitCost,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
	if m.ReasonID != nil {
		resp.ReasonID = strconv.FormatInt(*m.ReasonID, 10)
	}
	return resp
}

const maxReasonCodeLen = 32

const reasonI18nPrefix = "inventory.reason."

func builtinReasonKey(code string) string { return reasonI18nPrefix + strings.ToLower(code) }

func customReasonKey(projectID, code string) string {
	return reasonI18nPrefix + "custom." + strings.ToLower(strings.TrimSpace(projectID)) + "." + strings.ToLower(code)
}

const reasonTextCategory = "inventory"

func saveReasonText(ctx context.Context, key, text string) (err error) {
	return i18n.SaveEntry(ctx, i18n.Entry{
		Key:      key,
		Lang:     "zh-CN",
		Value:    text,
		Category: reasonTextCategory,
		Remark:   "internal/module/inventory/service/inventory_change.go",
	})
}

func writeReasonText(ctx context.Context, key, text string) (ok bool) {
	if err := saveReasonText(ctx, key, text); err != nil {
		logger.Scene("inventory").With("key", key).With("err", err).Warn("变动原因文案写入 sys_i18n 失败")
		return false
	}
	return true
}

func (s *Service) ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	filter := inventorymodel.ReasonFilter{
		Keyword:         strings.TrimSpace(req.Keyword),
		IncludeDisabled: req.IncludeDisabled,
	}
	if filter.Direction, err = normalizeDirectionOrEmpty(req.Direction); err != nil {
		return nil, err
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListReasons(ctx, filter)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.ReasonResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toReasonResp(r))
	}
	return list, nil
}

func (s *Service) CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrReasonNameRequired)
	}
	code, err := normalizeReasonCode(req.Code)
	if err != nil {
		return nil, err
	}
	direction, err := normalizeDirection(req.Direction)
	if err != nil {
		return nil, inventoryenums.Err(inventoryenums.ErrReasonDirectionInvalid)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	if taken, cerr := s.m.ReasonCodeExists(ctx, projectID, code, 0); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, inventoryenums.Err(inventoryenums.ErrReasonCodeTaken)
	}

	key := customReasonKey(projectID, code)
	writeReasonText(ctx, key, name)
	now := time.Now().UTC()
	pid := projectID
	e := &inventorymodel.ReasonEntity{
		ProjectID: &pid, Code: code, Name: key, Direction: direction,
		IsBuiltin: false, Status: inventoryenums.StatusActive, Sort: req.Sort,
		CreateTime: now, UpdatedAt: now,
	}
	if err = s.m.CreateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

func (s *Service) UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	rid, perr := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if perr != nil {
		return nil, inventoryenums.Err(inventoryenums.ErrReasonNotFound)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, err := s.m.GetReason(ctx, rid, projectID)
	if err != nil {
		return nil, mapReasonNotFound(err)
	}

	if e.IsBuiltin && req.Name != nil {
		return nil, inventoryenums.Err(inventoryenums.ErrReasonBuiltin)
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrReasonNameRequired)
		}

		writeReasonText(ctx, e.Name, name)
	}
	if req.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*req.Status))
		if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
			return nil, inventoryenums.Err(inventoryenums.ErrReasonStatusInvalid)
		}
		e.Status = status
	}
	if req.Sort != nil {
		e.Sort = *req.Sort
	}
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.UpdateReason(ctx, e); err != nil {
		return nil, err
	}
	return toReasonResp(e), nil
}

func (s *Service) resolveReason(ctx context.Context, projectID, code, direction string) (r *inventorymodel.ReasonEntity, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrStockReasonRequired)
	}
	e, ferr := s.m.FindReasonByCode(ctx, projectID, code)
	if ferr != nil {
		return nil, mapReasonNotFound(ferr)
	}
	if e.Direction != direction {
		return nil, inventoryenums.Err(inventoryenums.ErrReasonDirectionMismatch)
	}
	return e, nil
}

func normalizeReasonCode(code string) (out string, err error) {
	out = strings.ToLower(strings.TrimSpace(code))
	if out == "" {
		return "", inventoryenums.Err(inventoryenums.ErrReasonCodeRequired)
	}
	if len(out) > maxReasonCodeLen {
		return "", inventoryenums.Err(inventoryenums.ErrReasonCodeInvalid)
	}
	for _, r := range out {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return "", inventoryenums.Err(inventoryenums.ErrReasonCodeInvalid)
		}
	}
	return out, nil
}

func mapReasonNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return inventoryenums.Err(inventoryenums.ErrReasonNotFound)
	}
	return err
}

func toReasonResp(e *inventorymodel.ReasonEntity) *inventorydto.ReasonResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.ReasonResp{
		ID: strconv.FormatInt(e.ID, 10), Code: e.Code, Name: e.Name, Direction: e.Direction,
		IsBuiltin: e.IsBuiltin, Status: e.Status, Sort: e.Sort,
		CreatedAt: e.CreateTime.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if e.ProjectID != nil {
		resp.ProjectID = *e.ProjectID
	}
	return resp
}
