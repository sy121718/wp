package purchaseservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	purchasemodel "go_wp/internal/module/purchase/model"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	maxPurchaseLines = 100

	maxPurchaseCodeLen = 32

	defaultPurchasePageSize = 20
	maxPurchasePageSize     = 200

	reasonCodePurchaseIn   = "purchase_in"
	reasonCodeProductionIn = "production_in"
)

func (s *Service) CreatePurchaseOrder(ctx context.Context, req *purchasedto.CreatePurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error) {
	if req == nil {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	code, err := normalizePurchaseCode(req.Code)
	if err != nil {
		return nil, err
	}
	source, err := s.resolvePurchaseSource(ctx, projectID, req.SourceID)
	if err != nil {
		return nil, err
	}

	wh, err := s.stock.ResolveWarehouse(ctx, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	if taken, cerr := s.m.PurchaseCodeExists(ctx, projectID, code, ""); cerr != nil {
		return nil, cerr
	} else if taken {
		return nil, purchaseenums.Err(purchaseenums.ErrPurchaseCodeTaken)
	}

	orderID := uuid.NewString()

	lines, err := buildPurchaseLines(projectID, orderID, wh.Code, req.Lines)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	status := derivePurchaseStatus(lines)
	if req.AsDraft {
		status = purchaseenums.PurchaseStatusDraft
	}
	order := &purchasemodel.PurchaseOrderEntity{
		ID: orderID, ProjectID: projectID, Code: code,
		SourceID: source.ID, WarehouseID: wh.ID,
		Status:    status,
		OrderedAt: now, ExpectedAt: normalizeTime(req.ExpectedAt.TimePtr()),
		Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
		Metadata: orJSON(req.Metadata, "{}"), CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.m.CreatePurchaseOrderTx(ctx, tx, order, lines)
	}); err != nil {
		return nil, err
	}

	return s.purchaseOrderResp(ctx, order, lines, source, wh), nil
}

func (s *Service) UpdatePurchaseOrder(ctx context.Context, req *purchasedto.UpdatePurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	order, err := s.m.GetPurchaseOrder(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapPurchaseOrderNotFound(err)
	}

	projectID = order.ProjectID

	if prr := assertPurchaseEditable(order.Status); prr != nil {
		return nil, prr
	}
	if req.SourceID != nil {
		source, serr := s.resolvePurchaseSource(ctx, projectID, *req.SourceID)
		if serr != nil {
			return nil, serr
		}
		order.SourceID = source.ID
	}
	if req.WarehouseID != nil {
		wh, werr := s.stock.ResolveWarehouse(ctx, projectID, *req.WarehouseID)
		if werr != nil {
			return nil, werr
		}
		order.WarehouseID = wh.ID
	}

	var orderWarehouseCode string
	if req.ReplaceLines {
		orderWarehouse, werr := s.stock.ResolveWarehouse(ctx, projectID, order.WarehouseID)
		if werr != nil {
			return nil, werr
		}
		orderWarehouseCode = orderWarehouse.Code
	}
	if req.ExpectedAt != nil {
		order.ExpectedAt = normalizeTime(req.ExpectedAt.TimePtr())
	}
	if req.ClearExpectedAt {
		order.ExpectedAt = nil
	}
	if req.Remark != nil {
		order.Remark = strings.TrimSpace(*req.Remark)
	}
	if req.OperatorID != nil {
		order.OperatorID = strings.TrimSpace(*req.OperatorID)
	}
	if req.Metadata != nil {
		order.Metadata = orJSON(req.Metadata, "{}")
	}

	var lines []*purchasemodel.PurchaseLineEntity
	now := time.Now().UTC()
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {

		if _, lerr := s.m.LockPurchaseOrderTx(ctx, tx, order.ID, projectID); lerr != nil {
			return mapPurchaseOrderNotFound(lerr)
		}
		current, lerr := s.m.ListPurchaseLinesTx(ctx, tx, order.ID, projectID)
		if lerr != nil {
			return lerr
		}
		lines = current
		if req.ReplaceLines {
			for _, line := range current {
				if line.ReceivedQuantity > 0 {

					return purchaseenums.Err(purchaseenums.ErrPurchaseLinesLocked)
				}
			}
			replaced, berr := buildPurchaseLines(projectID, order.ID, orderWarehouseCode, req.Lines)
			if berr != nil {
				return berr
			}
			if rerr := s.m.ReplacePurchaseLinesTx(ctx, tx, order.ID, projectID, replaced); rerr != nil {
				return rerr
			}
			lines = replaced
		}

		order.Status = statusAfterDerive(order.Status, derivePurchaseStatus(lines))
		order.UpdatedAt = now
		return s.m.UpdatePurchaseOrderTx(ctx, tx, order)
	}); err != nil {
		return nil, err
	}
	return s.purchaseOrderResp(ctx, order, lines, nil, nil), nil
}

func (s *Service) PlacePurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error) {
	return s.settlePurchaseStatus(ctx, req, purchaseenums.PurchaseStatusOrdered)
}

func (s *Service) MarkPurchaseOrderException(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error) {
	return s.settlePurchaseStatus(ctx, req, purchaseenums.PurchaseStatusException)
}

func (s *Service) ResumePurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error) {
	return s.settlePurchaseStatus(ctx, req, purchaseTargetResume)
}

const purchaseTargetResume = "resume"

func (s *Service) FinishPurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error) {
	return s.settlePurchaseStatus(ctx, req, purchaseenums.PurchaseStatusFinished)
}

func (s *Service) CancelPurchaseOrder(ctx context.Context, req *purchasedto.PurchaseStatusReq) (res *purchasedto.PurchaseOrderResp, err error) {
	return s.settlePurchaseStatus(ctx, req, purchaseenums.PurchaseStatusCancelled)
}

func (s *Service) settlePurchaseStatus(ctx context.Context, req *purchasedto.PurchaseStatusReq,
	target string) (res *purchasedto.PurchaseOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	remark := strings.TrimSpace(req.Remark)
	now := time.Now().UTC()
	var order *purchasemodel.PurchaseOrderEntity
	var lines []*purchasemodel.PurchaseLineEntity
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.m.LockPurchaseOrderTx(ctx, tx, strings.TrimSpace(req.ID), projectID)
		if lerr != nil {
			return mapPurchaseOrderNotFound(lerr)
		}
		if perr := assertPurchaseTransition(locked.Status, target); perr != nil {
			return perr
		}
		list, qerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID, projectID)
		if qerr != nil {
			return qerr
		}
		next := target
		if target == purchaseTargetResume {

			next = derivePurchaseStatus(list)
		}

		if remark != "" {
			locked.Remark = orJoinRemark(locked.Remark, remark)
		}
		locked.Status = next
		locked.UpdatedAt = now
		if uerr := s.m.UpdatePurchaseOrderTx(ctx, tx, locked); uerr != nil {
			return uerr
		}
		order = locked
		lines = list
		return nil
	}); err != nil {
		return nil, err
	}
	return s.purchaseOrderResp(ctx, order, lines, nil, nil), nil
}

func assertPurchaseEditable(current string) error {
	switch current {
	case purchaseenums.PurchaseStatusFinished, purchaseenums.PurchaseStatusCancelled:
		return purchaseenums.Err(purchaseenums.ErrPurchaseStatusInvalid)
	}
	return nil
}

func assertPurchaseTransition(current, target string) error {
	switch target {
	case purchaseenums.PurchaseStatusOrdered:
		if current == purchaseenums.PurchaseStatusDraft {
			return nil
		}
		return purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotPlaceable)
	case purchaseenums.PurchaseStatusException:
		if isStatusIn(current, purchaseenums.PurchaseStatusOrdered,
			purchaseenums.PurchaseStatusPartial, purchaseenums.PurchaseStatusReceived) {
			return nil
		}
		return purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotExceptionable)
	case purchaseTargetResume:
		if current == purchaseenums.PurchaseStatusException {
			return nil
		}
		return purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotResumable)
	case purchaseenums.PurchaseStatusFinished:
		if isStatusIn(current, purchaseenums.PurchaseStatusReceived, purchaseenums.PurchaseStatusPartial) {
			return nil
		}
		return purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotFinishable)
	case purchaseenums.PurchaseStatusCancelled:
		if isStatusIn(current, purchaseenums.PurchaseStatusDraft, purchaseenums.PurchaseStatusOrdered,
			purchaseenums.PurchaseStatusPartial, purchaseenums.PurchaseStatusException) {
			return nil
		}
		return purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotCancelable)
	}
	return purchaseenums.Err(purchaseenums.ErrPurchaseStatusInvalid)
}

func isStatusIn(current string, candidates ...string) bool {
	for _, c := range candidates {
		if current == c {
			return true
		}
	}
	return false
}

func orJoinRemark(existing, extra string) string {
	if strings.TrimSpace(existing) == "" {
		return extra
	}
	return existing + "；" + extra
}

func (s *Service) GetPurchaseOrder(ctx context.Context, req *purchasedto.GetPurchaseOrderReq) (res *purchasedto.PurchaseOrderResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	order, err := s.m.GetPurchaseOrder(ctx, req.ID, projectID)
	if err != nil {
		return nil, mapPurchaseOrderNotFound(err)
	}
	lines, err := s.m.ListPurchaseLines(ctx, order.ID, order.ProjectID)
	if err != nil {
		return nil, err
	}
	return s.purchaseOrderResp(ctx, order, lines, nil, nil), nil
}

func (s *Service) ListPurchaseOrders(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (list []*purchasedto.PurchaseOrderResp, err error) {
	filter, page, size, err := s.purchaseOrderFilter(ctx, req)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListPurchaseOrderRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	orderIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		orderIDs = append(orderIDs, r.ID)
	}

	allLines, err := s.m.ListPurchaseLinesByOrders(ctx, orderIDs, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	byOrder := make(map[string][]*purchasemodel.PurchaseLineEntity, len(orderIDs))
	for _, line := range allLines {
		byOrder[line.OrderID] = append(byOrder[line.OrderID], line)
	}

	names := s.warehouseNames(ctx, filter.ProjectID)
	list = make([]*purchasedto.PurchaseOrderResp, 0, len(rows))
	for _, r := range rows {
		lines := byOrder[r.ID]
		resp := purchaseOrderRespFromRow(r)
		resp.WarehouseName = names[r.WarehouseID]
		resp.Lines = toPurchaseLineResps(lines)
		list = append(list, resp)
	}
	return list, nil
}

func (s *Service) CountPurchaseOrders(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (n int64, err error) {
	filter, _, _, err := s.purchaseOrderFilter(ctx, req)
	if err != nil {
		return 0, err
	}
	return s.m.CountPurchaseOrders(ctx, filter)
}

func (s *Service) purchaseOrderFilter(ctx context.Context, req *purchasedto.ListPurchaseOrderReq) (filter purchasemodel.PurchaseOrderFilter, page, size int, err error) {
	if req == nil {
		return filter, page, size, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	status, serr := normalizePurchaseStatusFilter(req.Status)
	if serr != nil {
		return filter, page, size, serr
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return filter, page, size, perr
	}
	filter = purchasemodel.PurchaseOrderFilter{
		ProjectID: projectID, Status: status,
		SourceID: strings.TrimSpace(req.SourceID), Keyword: strings.TrimSpace(req.Keyword),
	}
	page, size = purchasePageArgs(req.Page, req.Size)
	return filter, page, size, nil
}

func buildPurchaseLines(projectID, orderID, warehouseCode string, rows []purchasedto.PurchaseLineReq) (lines []*purchasemodel.PurchaseLineEntity, err error) {
	if len(rows) == 0 {
		return nil, purchaseenums.Err(purchaseenums.ErrPurchaseLinesRequired)
	}
	if len(rows) > maxPurchaseLines {
		return nil, purchaseenums.Err(purchaseenums.ErrPurchaseLinesTooMany)
	}
	now := time.Now().UTC()
	seen := make(map[string]bool, len(rows))
	lines = make([]*purchasemodel.PurchaseLineEntity, 0, len(rows))
	for i, row := range rows {
		variantID := strings.TrimSpace(row.VariantID)
		if variantID == "" {
			return nil, purchaseenums.Err(purchaseenums.ErrPurchaseLineRequired)
		}
		if seen[variantID] {

			return nil, purchaseenums.Err(purchaseenums.ErrPurchaseLineDuplicate)
		}
		seen[variantID] = true
		if row.Quantity <= 0 {
			return nil, purchaseenums.Err(purchaseenums.ErrPurchaseQuantityInvalid)
		}
		if row.UnitPrice <= 0 {
			return nil, purchaseenums.Err(purchaseenums.ErrPurchasePriceInvalid)
		}
		sortValue := row.Sort
		if sortValue == 0 {
			sortValue = i
		}

		skuCode, serr := normalizeStockSKU(row.SKUCode, warehouseCode)
		if serr != nil {
			return nil, serr
		}
		lines = append(lines, &purchasemodel.PurchaseLineEntity{
			ID: uuid.NewString(), OrderID: orderID, ProjectID: projectID,
			ProductID: strings.TrimSpace(row.ProductID), VariantID: variantID,
			SKUCode:  skuCode,
			Quantity: row.Quantity, ReceivedQuantity: 0, UnitPrice: row.UnitPrice,
			Sort: sortValue, Remark: strings.TrimSpace(row.Remark),
			Metadata: orJSON(nil, "{}"), CreatedAt: now, UpdatedAt: now,
		})
	}
	return lines, nil
}

func derivePurchaseStatus(lines []*purchasemodel.PurchaseLineEntity) (status string) {
	if len(lines) == 0 {
		return purchaseenums.PurchaseStatusOrdered
	}
	receivedAll := true
	receivedAny := false
	for _, line := range lines {
		if line.ReceivedQuantity >= line.Quantity {
			receivedAny = true
			continue
		}
		receivedAll = false
		if line.ReceivedQuantity > 0 {
			receivedAny = true
		}
	}
	switch {
	case receivedAll:
		return purchaseenums.PurchaseStatusReceived
	case receivedAny:
		return purchaseenums.PurchaseStatusPartial
	default:
		return purchaseenums.PurchaseStatusOrdered
	}
}

func statusAfterDerive(current, derived string) string {
	for _, s := range purchaseenums.PurchaseDerivedStatuses {
		if current == s {
			return derived
		}
	}
	return current
}

func (s *Service) resolvePurchaseSource(ctx context.Context, projectID, sourceID string) (e *purchasemodel.SourceEntity, err error) {
	id := strings.TrimSpace(sourceID)
	if id == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrPurchaseSourceRequired)
	}
	e, err = s.m.GetSource(ctx, id, projectID)
	if err != nil {
		return nil, mapSourceNotFound(err)
	}
	if e.ProjectID != projectID {
		return nil, purchaseenums.Err(purchaseenums.ErrSourceNotFound)
	}
	if e.Status != purchaseenums.SourceStatusActive {
		return nil, purchaseenums.Err(purchaseenums.ErrPurchaseSourceDisabled)
	}
	return e, nil
}

func (s *Service) purchaseOrderResp(ctx context.Context, e *purchasemodel.PurchaseOrderEntity,
	lines []*purchasemodel.PurchaseLineEntity, source *purchasemodel.SourceEntity,
	wh *purchasecontract.WarehouseRef) *purchasedto.PurchaseOrderResp {
	if source == nil {
		if got, err := s.m.GetSource(ctx, e.SourceID, e.ProjectID); err == nil {
			source = got
		}
	}
	if wh == nil {
		if got, err := s.stock.ResolveWarehouse(ctx, e.ProjectID, e.WarehouseID); err == nil {
			wh = got
		}
	}
	resp := &purchasedto.PurchaseOrderResp{
		ID: e.ID, ProjectID: e.ProjectID, Code: e.Code,
		SourceID: e.SourceID, WarehouseID: e.WarehouseID,
		Status: e.Status, OrderedAt: utils.NewJSONTime(e.OrderedAt), ExpectedAt: utils.NewJSONTimePtr(e.ExpectedAt),
		Remark: e.Remark, OperatorID: e.OperatorID,
		Lines: toPurchaseLineResps(lines), CreatedAt: utils.NewJSONTime(e.CreatedAt), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
	if source != nil {
		resp.SourceName, resp.SourceType = source.Name, source.Type
	}
	if wh != nil {
		resp.WarehouseName = wh.Name
	}
	fillPurchaseQuantities(resp, lines)
	return resp
}

func fillPurchaseQuantities(resp *purchasedto.PurchaseOrderResp, lines []*purchasemodel.PurchaseLineEntity) {
	resp.TotalQuantity, resp.ReceivedQuantity = sumPurchaseLines(lines)
	resp.OutstandingQuantity = resp.TotalQuantity - resp.ReceivedQuantity
	if resp.TotalQuantity > 0 {
		resp.ProgressPercent = resp.ReceivedQuantity * 100 / resp.TotalQuantity
	}
}

func purchaseOrderRespFromRow(row *purchasemodel.PurchaseOrderRow) *purchasedto.PurchaseOrderResp {
	resp := &purchasedto.PurchaseOrderResp{
		ID: row.ID, ProjectID: row.ProjectID, Code: row.Code,
		SourceID: row.SourceID, SourceName: row.SourceName, SourceType: row.SourceType,
		WarehouseID: row.WarehouseID, WarehouseName: row.WarehouseName,
		Status: row.Status, OrderedAt: utils.NewJSONTime(row.OrderedAt), ExpectedAt: utils.NewJSONTimePtr(row.ExpectedAt),
		Remark: row.Remark, OperatorID: row.OperatorID,
		TotalQuantity: row.TotalQuantity, ReceivedQuantity: row.ReceivedQuantity,
		CreatedAt: utils.NewJSONTime(row.CreatedAt), UpdatedAt: utils.NewJSONTime(row.UpdatedAt),
	}
	resp.OutstandingQuantity = row.TotalQuantity - row.ReceivedQuantity
	if row.TotalQuantity > 0 {
		resp.ProgressPercent = row.ReceivedQuantity * 100 / row.TotalQuantity
	}
	return resp
}

func toPurchaseLineResps(lines []*purchasemodel.PurchaseLineEntity) []*purchasedto.PurchaseLineResp {
	out := make([]*purchasedto.PurchaseLineResp, 0, len(lines))
	for _, line := range lines {
		out = append(out, &purchasedto.PurchaseLineResp{
			ID: line.ID, ProductID: line.ProductID, VariantID: line.VariantID,
			SKUCode: line.SKUCode, Quantity: line.Quantity,
			ReceivedQuantity:    line.ReceivedQuantity,
			OutstandingQuantity: line.Quantity - line.ReceivedQuantity,
			UnitPrice:           line.UnitPrice, Sort: line.Sort, Remark: line.Remark,
		})
	}
	return out
}

func sumPurchaseLines(lines []*purchasemodel.PurchaseLineEntity) (total, received int) {
	for _, line := range lines {
		total += line.Quantity
		received += line.ReceivedQuantity
	}
	return total, received
}

func normalizePurchaseCode(code string) (out string, err error) {
	out = strings.ToUpper(strings.TrimSpace(code))
	if out == "" {
		return "", purchaseenums.Err(purchaseenums.ErrPurchaseCodeRequired)
	}
	if len(out) > maxPurchaseCodeLen {
		return "", purchaseenums.Err(purchaseenums.ErrPurchaseCodeInvalid)
	}
	for _, r := range out {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "", purchaseenums.Err(purchaseenums.ErrPurchaseCodeInvalid)
	}
	return out, nil
}

func normalizePurchaseStatusFilter(status string) (out string, err error) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	if normalized == "" {
		return "", nil
	}
	if !purchaseenums.IsKnownPurchaseStatus(normalized) {
		return "", purchaseenums.Err(purchaseenums.ErrPurchaseStatusInvalid)
	}
	return normalized, nil
}

func purchasePageArgs(page, size int) (outPage, outSize int) {
	outPage, outSize = 1, defaultPurchasePageSize
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
		if outSize > maxPurchasePageSize {
			outSize = maxPurchasePageSize
		}
	}
	return outPage, outSize
}

func normalizeTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func mapPurchaseOrderNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return purchaseenums.Err(purchaseenums.ErrPurchaseOrderNotFound)
	}
	return err
}

const (
	maxRequestIDLen = 64

	defaultHistoryPageSize = 50
	maxHistoryPageSize     = 200

	maxReceiptLines = 200
)

func (s *Service) RegisterReceipt(ctx context.Context, req *purchasedto.RegisterReceiptReq) (res *purchasedto.ReceiptResp, err error) {
	if req == nil {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	requestID, err := normalizeRequestID(req.RequestID)
	if err != nil {
		return nil, err
	}

	replay, err := s.idempotentReceipt(ctx, projectID, requestID)
	if err != nil {
		return nil, err
	}
	if replay != nil {

		s.replayReceiptCosts(ctx, replay)
		return s.receiptResp(ctx, replay, true)
	}
	if len(req.Lines) == 0 {
		return nil, purchaseenums.Err(purchaseenums.ErrReceiptLinesRequired)
	}
	if len(req.Lines) > maxReceiptLines {
		return nil, purchaseenums.Err(purchaseenums.ErrStockLinesTooMany)
	}
	receivedAt, now := time.Now().UTC(), time.Now().UTC()
	if req.ReceivedAt != nil {
		receivedAt = req.ReceivedAt.Time().UTC()
	}

	var (
		receipt    *purchasemodel.ReceiptEntity
		items      []*purchasemodel.ReceiptItemEntity
		stockLines []purchasecontract.StockLine
	)
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {

		locked, lerr := s.m.LockPurchaseOrderTx(ctx, tx, strings.TrimSpace(req.OrderID), projectID)
		if lerr != nil {
			return mapPurchaseOrderNotFound(lerr)
		}
		if locked.ProjectID != projectID {
			return purchaseenums.Err(purchaseenums.ErrPurchaseOrderNotFound)
		}
		source, lerr := s.resolvePurchaseSource(ctx, projectID, locked.SourceID)
		if lerr != nil {
			return lerr
		}

		wantWarehouse := strings.TrimSpace(req.WarehouseID)
		if wantWarehouse == "" {
			wantWarehouse = locked.WarehouseID
		}
		wh, lerr := s.stock.ResolveWarehouse(ctx, projectID, wantWarehouse)
		if lerr != nil {
			return lerr
		}
		lines, lerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID, projectID)
		if lerr != nil {
			return lerr
		}
		if len(lines) == 0 {
			return purchaseenums.Err(purchaseenums.ErrPurchaseLinesRequired)
		}
		byID := make(map[string]*purchasemodel.PurchaseLineEntity, len(lines))
		outstanding := false
		for _, line := range lines {
			byID[line.ID] = line
			if line.ReceivedQuantity < line.Quantity {
				outstanding = true
			}
		}
		if !outstanding {

			return purchaseenums.Err(purchaseenums.ErrReceiptOrderDone)
		}

		if !purchaseenums.IsReceivableStatus(locked.Status) {
			return purchaseenums.Err(purchaseenums.ErrPurchaseStatusNotReceivable)
		}
		receiptID := uuid.NewString()
		seq, cerr := s.m.CountOrderReceiptsTx(ctx, tx, locked.ID, projectID)
		if cerr != nil {
			return cerr
		}
		items = make([]*purchasemodel.ReceiptItemEntity, 0, len(req.Lines))
		stockLines = make([]purchasecontract.StockLine, 0, len(req.Lines))
		seenLineIDs := make(map[string]struct{}, len(req.Lines))
		for _, row := range req.Lines {
			lineID := strings.TrimSpace(row.LineID)
			if _, exists := seenLineIDs[lineID]; exists {
				return purchaseenums.Err(purchaseenums.ErrReceiptLineDuplicate)
			}
			seenLineIDs[lineID] = struct{}{}
			line, ok := byID[lineID]
			if !ok {
				return purchaseenums.Err(purchaseenums.ErrPurchaseLineNotFound)
			}
			if row.Quantity <= 0 {
				return purchaseenums.Err(purchaseenums.ErrReceiptQuantityInvalid)
			}

			skuCode, nerr := normalizeStockSKU(line.SKUCode, wh.Code)
			if nerr != nil {
				return nerr
			}

			unitPrice := line.UnitPrice
			if row.UnitPrice != nil {
				if *row.UnitPrice <= 0 {
					return purchaseenums.Err(purchaseenums.ErrPurchasePriceInvalid)
				}
				unitPrice = *row.UnitPrice
			}

			affected, ierr := s.m.IncrPurchaseLineReceivedTx(ctx, tx, line.ID, row.Quantity, projectID, now)
			if ierr != nil {
				return ierr
			}
			if affected == 0 {
				return purchaseenums.Err(purchaseenums.ErrReceiptOverReceive)
			}
			receiptLineID := line.ID
			items = append(items, &purchasemodel.ReceiptItemEntity{
				ID: uuid.NewString(), ReceiptID: receiptID, ProjectID: projectID,
				LineID: &receiptLineID, ProductID: line.ProductID, VariantID: line.VariantID,
				SKUCode: skuCode, Quantity: row.Quantity, UnitPrice: unitPrice,
				CostUpdated: false, CostError: "", CreatedAt: now,
			})

			lineCost := unitPrice
			stockLines = append(stockLines, purchasecontract.StockLine{
				VariantID: line.VariantID, ProductID: line.ProductID,
				SKUCode: skuCode, WarehouseID: wh.ID, Quantity: row.Quantity,
				CostPrice: &lineCost,
			})
		}

		fresh, lerr := s.m.ListPurchaseLinesTx(ctx, tx, locked.ID, projectID)
		if lerr != nil {
			return lerr
		}
		status := statusAfterDerive(locked.Status, derivePurchaseStatus(fresh))
		receipt = &purchasemodel.ReceiptEntity{
			ID: receiptID, ProjectID: projectID,
			Code: fmt.Sprintf("%s-R%d", locked.Code, seq+1),
			Kind: purchaseenums.ReceiptKindPurchase, OrderID: &locked.ID,
			SourceID: source.ID, WarehouseID: wh.ID, RequestID: requestID,
			Status: purchaseenums.ReceiptStatusPending,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			ReceivedAt: receivedAt, Metadata: orJSON(nil, "{}"), CreatedAt: now,
		}
		if cerr := s.m.CreateReceiptTx(ctx, tx, receipt, items); cerr != nil {
			return cerr
		}
		if uerr := s.m.UpdatePurchaseOrderStatusTx(ctx, tx, locked.ID, status, projectID, now); uerr != nil {
			return uerr
		}

		batchID, cerr := s.stock.ChangeStockTx(ctx, tx, &purchasecontract.StockChange{
			ProjectID: projectID, Direction: purchaseenums.DirectionIn,
			ReasonCode: reasonCodePurchaseIn,
			SourceType: purchaseenums.MovementSourcePurchaseOrder, SourceRef: locked.Code,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			Lines: stockLines,
		})
		if cerr != nil {
			return cerr
		}

		receipt.MovementBatchID = batchID
		receipt.Status = purchaseenums.ReceiptStatusPosted
		return s.m.SetReceiptMovementTx(ctx, tx, receipt.ID, batchID,
			purchaseenums.ReceiptStatusPosted, projectID)
	})
	if err != nil {

		if database.IsUniqueViolation(err) {
			if replay, rerr := s.idempotentReceipt(ctx, projectID, requestID); rerr == nil && replay != nil {
				s.replayReceiptCosts(ctx, replay)
				return s.receiptResp(ctx, replay, true)
			}
		}
		return nil, err
	}

	s.applyReceiptCosts(ctx, items, receipt.OperatorID)
	return s.receiptResp(ctx, receipt, false)
}

func (s *Service) RegisterProductionInbound(ctx context.Context, req *purchasedto.ProductionInboundReq) (res *purchasedto.ReceiptResp, err error) {
	if req == nil {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	requestID, err := normalizeRequestID(req.RequestID)
	if err != nil {
		return nil, err
	}
	replay, err := s.idempotentReceipt(ctx, projectID, requestID)
	if err != nil {
		return nil, err
	}
	if replay != nil {

		s.replayReceiptCosts(ctx, replay)
		return s.receiptResp(ctx, replay, true)
	}
	variantID := strings.TrimSpace(req.VariantID)
	if variantID == "" {
		return nil, purchaseenums.Err(purchaseenums.ErrProductionVariantRequired)
	}
	if req.Quantity <= 0 {
		return nil, purchaseenums.Err(purchaseenums.ErrReceiptQuantityInvalid)
	}
	if req.UnitCost == nil || *req.UnitCost < 0 {

		return nil, purchaseenums.Err(purchaseenums.ErrProductionCostInvalid)
	}
	source, err := s.resolvePurchaseSource(ctx, projectID, req.SourceID)
	if err != nil {
		return nil, err
	}
	if source.Type != purchaseenums.SourceTypeInternal {

		return nil, purchaseenums.Err(purchaseenums.ErrProductionSourceNotInternal)
	}
	wh, err := s.stock.ResolveWarehouse(ctx, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}

	skuCode, err := normalizeStockSKU(req.SKUCode, wh.Code)
	if err != nil {
		return nil, err
	}
	receivedAt, now := time.Now().UTC(), time.Now().UTC()
	if req.ReceivedAt != nil {
		receivedAt = req.ReceivedAt.Time().UTC()
	}
	receiptID := uuid.NewString()
	code := productionReceiptCode(receiptID)
	receipt := &purchasemodel.ReceiptEntity{
		ID: receiptID, ProjectID: projectID, Code: code,
		Kind: purchaseenums.ReceiptKindProduction, OrderID: nil,
		SourceID: source.ID, WarehouseID: wh.ID, RequestID: requestID,
		Status: purchaseenums.ReceiptStatusPending,
		Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
		ReceivedAt: receivedAt, Metadata: orJSON(nil, "{}"), CreatedAt: now,
	}
	item := &purchasemodel.ReceiptItemEntity{
		ID: uuid.NewString(), ReceiptID: receiptID, ProjectID: projectID, LineID: nil,
		ProductID: strings.TrimSpace(req.ProductID), VariantID: variantID,
		SKUCode: skuCode, Quantity: req.Quantity,
		UnitPrice: *req.UnitCost, CostUpdated: false, CostError: "", CreatedAt: now,
	}

	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateReceiptTx(ctx, tx, receipt, []*purchasemodel.ReceiptItemEntity{item}); cerr != nil {
			return cerr
		}
		batchID, cerr := s.stock.ChangeStockTx(ctx, tx, &purchasecontract.StockChange{
			ProjectID: projectID, Direction: purchaseenums.DirectionIn,
			ReasonCode: reasonCodeProductionIn,
			SourceType: purchaseenums.MovementSourceProduction, SourceRef: code,
			Remark: strings.TrimSpace(req.Remark), OperatorID: strings.TrimSpace(req.OperatorID),
			Lines: []purchasecontract.StockLine{{
				VariantID: variantID, ProductID: strings.TrimSpace(req.ProductID),
				SKUCode: skuCode, WarehouseID: wh.ID, Quantity: req.Quantity,

				CostPrice: req.UnitCost,
			}},
		})
		if cerr != nil {
			return cerr
		}
		receipt.MovementBatchID = batchID
		receipt.Status = purchaseenums.ReceiptStatusPosted
		return s.m.SetReceiptMovementTx(ctx, tx, receipt.ID, batchID,
			purchaseenums.ReceiptStatusPosted, projectID)
	}); err != nil {
		if database.IsUniqueViolation(err) {
			if replay, rerr := s.idempotentReceipt(ctx, projectID, requestID); rerr == nil && replay != nil {
				s.replayReceiptCosts(ctx, replay)
				return s.receiptResp(ctx, replay, true)
			}
		}
		return nil, err
	}
	s.applyReceiptCosts(ctx, []*purchasemodel.ReceiptItemEntity{item}, receipt.OperatorID)
	return s.receiptResp(ctx, receipt, false)
}

func (s *Service) ListPurchaseHistory(ctx context.Context, req *purchasedto.ListPurchaseHistoryReq) (list []*purchasedto.PurchaseHistoryResp, err error) {
	if req == nil {
		return nil, purchaseenums.Err(purchaseenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	filter := purchasemodel.HistoryFilter{
		ProjectID: projectID,
		SKUCode:   strings.TrimSpace(req.SKUCode),
		VariantID: strings.TrimSpace(req.VariantID),
		SourceID:  strings.TrimSpace(req.SourceID),
		OrderID:   strings.TrimSpace(req.OrderID),
	}
	page, size := historyPageArgs(req.Page, req.Size)
	rows, err := s.m.ListHistoryRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	names := s.warehouseNames(ctx, projectID)
	list = make([]*purchasedto.PurchaseHistoryResp, 0, len(rows))
	for _, r := range rows {
		orderID := ""
		if r.OrderID != nil {
			orderID = *r.OrderID
		}
		list = append(list, &purchasedto.PurchaseHistoryResp{
			ReceiptID: r.ReceiptID, ReceiptCode: r.ReceiptCode, Kind: r.Kind,
			OrderID: orderID, OrderCode: r.OrderCode,
			SourceID: r.SourceID, SourceName: r.SourceName, SourceType: r.SourceType,
			WarehouseID: r.WarehouseID, WarehouseName: names[r.WarehouseID],
			VariantID: r.VariantID, SKUCode: r.SKUCode,
			Quantity: r.Quantity, UnitPrice: r.UnitPrice, CostUpdated: r.CostUpdated,
			MovementBatchID: r.MovementBatchID, Remark: r.Remark,
			OperatorID: r.OperatorID, ReceivedAt: utils.NewJSONTime(r.ReceivedAt),
		})
	}
	return list, nil
}

func (s *Service) idempotentReceipt(ctx context.Context, projectID, requestID string) (e *purchasemodel.ReceiptEntity, err error) {
	if requestID == "" {
		return nil, nil
	}
	got, gerr := s.m.FindReceiptByRequestID(ctx, projectID, requestID)
	if gerr == nil {
		return got, nil
	}
	if errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return nil, gerr
}

func (s *Service) applyReceiptCosts(ctx context.Context, items []*purchasemodel.ReceiptItemEntity, operatorID string) {
	for _, it := range items {

		if it.CostUpdated {
			continue
		}
		if s.variantCost == nil {
			it.CostUpdated, it.CostError = false, purchaseenums.ErrVariantCostPortMissing
			s.recordReceiptCost(ctx, it)
			continue
		}
		if cerr := s.variantCost.UpdateVariantCost(ctx, it.ProjectID, it.VariantID, it.UnitPrice, operatorID); cerr != nil {
			it.CostUpdated, it.CostError = false, cerr.Error()
		} else {
			it.CostUpdated, it.CostError = true, ""
		}
		s.recordReceiptCost(ctx, it)
	}
}

func (s *Service) recordReceiptCost(ctx context.Context, it *purchasemodel.ReceiptItemEntity) {
	if err := s.m.UpdateReceiptItemCost(ctx, it.ID, it.CostUpdated, it.CostError, it.ProjectID); err != nil {
		logger.Scene("inventory").With("receipt_item_id", it.ID).With("variant_id", it.VariantID).
			Error(err, "入库单行的成本回写结果写入失败（该行的 cost_updated 与事实不符，需人工核对）")
	}
}

func (s *Service) replayReceiptCosts(ctx context.Context, receipt *purchasemodel.ReceiptEntity) {
	if receipt == nil {
		return
	}
	items, err := s.m.ListReceiptItems(ctx, receipt.ID, receipt.ProjectID)
	if err != nil {
		logger.Scene("inventory").With("receipt_id", receipt.ID).
			Error(err, "重放成本回写前读取入库单行失败")
		return
	}
	s.applyReceiptCosts(ctx, items, receipt.OperatorID)
}

func (s *Service) receiptResp(ctx context.Context, e *purchasemodel.ReceiptEntity, idempotent bool) (res *purchasedto.ReceiptResp, err error) {
	items, err := s.m.ListReceiptItems(ctx, e.ID, e.ProjectID)
	if err != nil {
		return nil, err
	}
	resp := &purchasedto.ReceiptResp{
		ID: e.ID, Code: e.Code, Kind: e.Kind, SourceID: e.SourceID,
		WarehouseID: e.WarehouseID, Status: e.Status, MovementBatchID: e.MovementBatchID,
		Remark: e.Remark, OperatorID: e.OperatorID, ReceivedAt: utils.NewJSONTime(e.ReceivedAt),
		Idempotent: idempotent, CostUpdated: true,
		Items: make([]*purchasedto.ReceiptItemResp, 0, len(items)),
	}
	if e.OrderID != nil {
		resp.OrderID = *e.OrderID
		if order, oerr := s.m.GetPurchaseOrder(ctx, *e.OrderID, e.ProjectID); oerr == nil {
			resp.OrderCode = order.Code
		}
	}
	if source, serr := s.m.GetSource(ctx, e.SourceID, e.ProjectID); serr == nil {
		resp.SourceName = source.Name
	}
	if wh, werr := s.stock.ResolveWarehouse(ctx, e.ProjectID, e.WarehouseID); werr == nil {
		resp.WarehouseName = wh.Name
	}
	for _, it := range items {
		lineID := ""
		if it.LineID != nil {
			lineID = *it.LineID
		}
		if !it.CostUpdated {
			resp.CostUpdated = false
		}
		resp.Items = append(resp.Items, &purchasedto.ReceiptItemResp{
			LineID: lineID, ProductID: it.ProductID, VariantID: it.VariantID,
			SKUCode: it.SKUCode, Quantity: it.Quantity, UnitPrice: it.UnitPrice,
			CostUpdated: it.CostUpdated, CostError: it.CostError,
		})
	}
	return resp, nil
}

func normalizeRequestID(raw string) (out string, err error) {
	out = strings.TrimSpace(raw)
	if out == "" {
		return "", nil
	}
	if len(out) > maxRequestIDLen {
		return "", purchaseenums.Err(purchaseenums.ErrReceiptRequestInvalid)
	}
	for _, r := range out {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' {
			continue
		}
		return "", purchaseenums.Err(purchaseenums.ErrReceiptRequestInvalid)
	}
	return out, nil
}

func productionReceiptCode(receiptID string) (code string) {
	short := strings.ToUpper(strings.ReplaceAll(receiptID, "-", ""))
	if len(short) > 8 {
		short = short[:8]
	}
	return "PROD-" + short
}

func historyPageArgs(page, size int) (outPage, outSize int) {
	outPage, outSize = 1, defaultHistoryPageSize
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
		if outSize > maxHistoryPageSize {
			outSize = maxHistoryPageSize
		}
	}
	return outPage, outSize
}

func (s *Service) warehouseNames(ctx context.Context, projectID string) map[string]string {
	if s.stock == nil || projectID == "" {
		return map[string]string{}
	}
	names, err := s.stock.WarehouseNames(ctx, projectID)
	if err != nil {
		return map[string]string{}
	}
	return names
}
