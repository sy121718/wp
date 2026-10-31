package inventoryservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

const (
	TransferStatusRequested = "requested"
	TransferStatusPending   = "pending"
	TransferStatusInTransit = "in_transit"
	TransferStatusCompleted = "completed"
	TransferStatusRejected  = "rejected"
	TransferStatusCancelled = "cancelled"
)

const maxTransferRejectReasonLen = 500

const (
	defaultTransferStockOptionLimit = 200
	maxTransferStockOptionLimit     = 500
)

const (
	TransferAttachmentRequest = "request"
	TransferAttachmentShip    = "ship"
	TransferAttachmentReceive = "receive"
)

func (s *Service) CreateTransfer(ctx context.Context, req *inventorydto.CreateTransferReq) (res *inventorydto.TransferResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	requestID := strings.TrimSpace(req.RequestID)
	if existing, ferr := s.m.FindTransferByRequestID(ctx, projectID, requestID); ferr != nil {
		return nil, ferr
	} else if existing != nil {
		return s.transferRespTx(ctx, nil, projectID, existing.ID)
	}
	fromWh, err := s.resolveWarehouse(ctx, projectID, req.FromWarehouseID)
	if err != nil {
		return nil, err
	}
	toWh, err := s.resolveWarehouse(ctx, projectID, req.ToWarehouseID)
	if err != nil {
		return nil, err
	}
	if fromWh.ID == toWh.ID {

		return nil, inventoryenums.Err(inventoryenums.ErrTransferSameWarehouse)
	}
	if len(req.Lines) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrTransferLinesRequired)
	}

	requestAtts, err := s.resolveAttachmentIDs(ctx, projectID, req.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	transferID := uuid.NewString()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		items, ierr := s.transferItemsTx(ctx, tx, projectID, fromWh, transferID, req.Lines)
		if ierr != nil {
			return ierr
		}
		no, nerr := s.nextTransferNoTx(ctx, tx, projectID, now)
		if nerr != nil {
			return nerr
		}
		atts := make([]*inventorymodel.TransferAttachmentEntity, 0, len(requestAtts))
		for i, att := range requestAtts {
			atts = append(atts, &inventorymodel.TransferAttachmentEntity{
				AttachmentID: att, UsageType: TransferAttachmentRequest, Sort: i, CreateTime: now,
			})
		}
		return s.m.CreateTransferTx(ctx, tx, &inventorymodel.TransferEntity{
			ID: transferID, ProjectID: projectID, TransferNo: no,
			FromWarehouseID: fromWh.ID, ToWarehouseID: toWh.ID,
			Status: TransferStatusRequested, Remark: strings.TrimSpace(req.Remark),
			CreateBy: req.OperatorID, RequestID: requestID,

			RequestedBy: req.OperatorID, RequestedAt: &now,
			CreateTime: now, UpdateTime: now,
		}, items, atts)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, transferID)
}

func (s *Service) transferItemsTx(ctx context.Context, tx *gorm.DB, projectID string,
	fromWh *inventorymodel.WarehouseEntity, transferID string,
	lines []inventorydto.TransferLineReq) (items []*inventorymodel.TransferItemEntity, err error) {
	if len(lines) > maxBatchLines {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}

	rows, err := s.m.ListStocksByWarehouseTx(ctx, tx, projectID, fromWh.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*inventorymodel.StockEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	items = make([]*inventorymodel.TransferItemEntity, 0, len(lines))
	seen := make(map[string]bool, len(lines))

	wanted := make(map[string]int, len(lines))
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		sid := strings.TrimSpace(line.StockID)
		if sid == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
		}
		if line.Quantity <= 0 {
			return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
		}
		if _, ok := wanted[sid]; !ok {
			order = append(order, sid)
		}
		wanted[sid] += line.Quantity
		seen[sid] = true
	}
	for _, sid := range order {
		row, ok := byID[sid]
		if !ok {

			return nil, inventoryenums.Err(inventoryenums.ErrItemNotInSourceWarehouse)
		}
		want := wanted[sid]
		if availableOf(row.Quantity, row.ReservedQuantity) < want {
			return nil, inventoryenums.Err(inventoryenums.ErrStockNotEnough)
		}
		items = append(items, &inventorymodel.TransferItemEntity{
			TransferID: transferID, StockID: row.ID,
			VariantID: row.VariantID, SKUCode: row.SKUCode, Quantity: want,
		})
	}
	_ = seen
	return items, nil
}

func (s *Service) ShipTransfer(ctx context.Context, req *inventorydto.TransferShipReq) (res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}

	if len(req.AttachmentIDs) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)
	}
	ids, aerr := s.resolveAttachmentIDs(ctx, projectID, req.AttachmentIDs)
	if aerr != nil {
		return nil, aerr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusPending {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		items, ierr := s.m.ListTransferItemsTx(ctx, tx, projectID, e.ID)
		if ierr != nil {
			return ierr
		}

		lines := make([]inventorydto.StockChangeLineReq, 0, len(items))
		for _, it := range items {
			var variantID string
			if it.VariantID != nil {
				variantID = *it.VariantID
			}
			lines = append(lines, inventorydto.StockChangeLineReq{
				WarehouseID: e.FromWarehouseID,
				VariantID:   variantID,
				SKUCode:     it.SKUCode,
				Quantity:    it.Quantity,
			})
		}

		if lerr := s.transferOutTx(ctx, tx, e, items, lines); lerr != nil {
			return lerr
		}
		if e, uerr := s.m.GetTransferTx(ctx, tx, projectID, e.ID); uerr == nil {
			_ = e
		}
		e.Status = TransferStatusInTransit
		e.ShippedBy = req.OperatorID
		e.ShippedAt = &now
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		if uerr := s.m.UpdateTransferTx(ctx, tx, e); uerr != nil {
			return uerr
		}

		atts := make([]*inventorymodel.TransferAttachmentEntity, 0, len(ids))
		for i, att := range ids {
			atts = append(atts, &inventorymodel.TransferAttachmentEntity{
				TransferID: e.ID, AttachmentID: att,
				UsageType: TransferAttachmentShip, Sort: i, CreateTime: now,
			})
		}
		return s.m.CreateTransferAttachmentsTx(ctx, tx, projectID, atts)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

func (s *Service) transferOutTx(ctx context.Context, tx *gorm.DB, e *inventorymodel.TransferEntity,
	items []*inventorymodel.TransferItemEntity, lines []inventorydto.StockChangeLineReq) error {

	srcRows, serr := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.FromWarehouseID)
	if serr != nil {
		return serr
	}
	byID := make(map[string]*inventorymodel.StockEntity, len(srcRows))
	for _, r := range srcRows {
		byID[r.ID] = r
	}
	claimed := make([]inventorymodel.TransferItemEntity, 0, len(items))
	for _, it := range items {
		if it.VariantID != nil && strings.TrimSpace(*it.VariantID) != "" {
			claimed = append(claimed, *it)
			continue
		}

		if err := s.deductUnclaimedStockTx(ctx, tx, e.ProjectID, it.StockID, it.Quantity); err != nil {
			return err
		}
	}
	if len(claimed) == 0 {
		return nil
	}
	claimLines := make([]inventorydto.StockChangeLineReq, 0, len(claimed))
	for _, it := range claimed {
		var variantID, productID string
		if it.VariantID != nil {
			variantID = *it.VariantID
		}
		if src := byID[it.StockID]; src != nil {
			productID = inventorymodel.PtrString(src.ProductID)
		}
		claimLines = append(claimLines, inventorydto.StockChangeLineReq{
			WarehouseID: e.FromWarehouseID, VariantID: variantID, ProductID: productID,
			SKUCode: it.SKUCode, Quantity: it.Quantity,
		})
	}
	return s.DeductStockTx(ctx, tx, &inventorydto.DeductStockReq{
		ProjectID: e.ProjectID, ReasonCode: "transfer_out",
		SourceType: "transfer", SourceRef: e.TransferNo,
		Remark: "调拨出库 → " + e.ToWarehouseID,
		Lines:  claimLines,
	})
}

func (s *Service) deductUnclaimedStockTx(ctx context.Context, tx *gorm.DB, projectID, stockID string, quantity int) error {
	affected, err := s.m.DeductUnclaimedStockTx(ctx, tx, projectID, stockID, quantity, time.Now().UTC())
	if err != nil {
		return err
	}
	if affected == 0 {
		return inventoryenums.Err(inventoryenums.ErrStockNotEnough)
	}
	return nil
}

func (s *Service) ReceiveTransfer(ctx context.Context, req *inventorydto.TransferReceiveReq) (res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	if len(req.AttachmentIDs) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrAttachmentRequired)
	}
	ids, aerr := s.resolveAttachmentIDs(ctx, projectID, req.AttachmentIDs)
	if aerr != nil {
		return nil, aerr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusInTransit {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		items, ierr := s.m.ListTransferItemsTx(ctx, tx, projectID, e.ID)
		if ierr != nil {
			return ierr
		}
		if lerr := s.transferInTx(ctx, tx, e, items); lerr != nil {
			return lerr
		}
		e.Status = TransferStatusCompleted
		e.ReceivedBy = req.OperatorID
		e.ReceivedAt = &now
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		if uerr := s.m.UpdateTransferTx(ctx, tx, e); uerr != nil {
			return uerr
		}
		atts := make([]*inventorymodel.TransferAttachmentEntity, 0, len(ids))
		for i, att := range ids {
			atts = append(atts, &inventorymodel.TransferAttachmentEntity{
				TransferID: e.ID, AttachmentID: att,
				UsageType: TransferAttachmentReceive, Sort: i, CreateTime: now,
			})
		}
		return s.m.CreateTransferAttachmentsTx(ctx, tx, projectID, atts)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

func (s *Service) transferInTx(ctx context.Context, tx *gorm.DB, e *inventorymodel.TransferEntity,
	items []*inventorymodel.TransferItemEntity) error {
	rows, err := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.ToWarehouseID)
	if err != nil {
		return err
	}

	srcRows, serr := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.FromWarehouseID)
	if serr != nil {
		return serr
	}

	type key struct{ variant, sku string }
	byKey := make(map[key]*inventorymodel.StockEntity, len(rows))
	for _, r := range rows {
		v := ""
		if r.VariantID != nil {
			v = *r.VariantID
		}
		byKey[key{variant: v, sku: r.SKUCode}] = r
	}
	now := time.Now().UTC()
	toCreate := make([]*inventorymodel.StockEntity, 0, len(items))
	toIncrease := make(map[string]int, len(items))
	for _, it := range items {
		v := ""
		if it.VariantID != nil {
			v = *it.VariantID
		}
		existing, ok := byKey[key{variant: v, sku: it.SKUCode}]
		if ok {
			toIncrease[existing.ID] += it.Quantity
			continue
		}

		toCreate = append(toCreate, &inventorymodel.StockEntity{
			ID: uuid.NewString(), ProjectID: e.ProjectID, WarehouseID: e.ToWarehouseID,

			ProductID: srcProductID(srcRows, it.StockID), VariantID: it.VariantID,
			SKUCode: it.SKUCode, TrackQuantity: true, Quantity: it.Quantity,
			Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
		})
	}
	for _, row := range toCreate {
		if cerr := s.m.CreateStockRowTx(ctx, tx, row); cerr != nil {
			return cerr
		}
	}
	for stockID, qty := range toIncrease {
		if aerr := s.m.IncreaseStockQuantityTx(ctx, tx, stockID, qty, now); aerr != nil {
			return aerr
		}
	}
	return nil
}

func srcProductID(rows []*inventorymodel.StockEntity, stockID string) *string {
	for _, r := range rows {
		if r.ID == stockID {
			return r.ProductID
		}
	}
	return nil
}

func (s *Service) ApproveTransfer(ctx context.Context, req *inventorydto.TransferApproveReq) (
	res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusRequested {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		items, ierr := s.m.ListTransferItemsTx(ctx, tx, projectID, e.ID)
		if ierr != nil {
			return ierr
		}
		if lerr := s.assertTransferAvailableTx(ctx, tx, e, items); lerr != nil {
			return lerr
		}
		if toID := strings.TrimSpace(req.ToWarehouseID); toID != "" && toID != e.ToWarehouseID {
			toWh, werr := s.resolveWarehouse(ctx, projectID, toID)
			if werr != nil {
				return werr
			}
			if toWh.ID == e.FromWarehouseID {
				return inventoryenums.Err(inventoryenums.ErrTransferSameWarehouse)
			}
			e.ToWarehouseID = toWh.ID
		}
		e.Status = TransferStatusPending
		e.ApprovedBy = req.OperatorID
		e.ApprovedName = strings.TrimSpace(req.OperatorName)
		e.ApprovedAt = &now
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		return s.m.UpdateTransferTx(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

func (s *Service) RejectTransfer(ctx context.Context, req *inventorydto.TransferRejectReq) (
	res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrTransferRejectReasonRequired)
	}
	if len([]rune(reason)) > maxTransferRejectReasonLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrTransferRejectReasonTooLong,
			"最多 "+strconv.Itoa(maxTransferRejectReasonLen)+" 个字符")
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}
		if e.Status != TransferStatusRequested {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		e.Status = TransferStatusRejected
		e.RejectedBy = req.OperatorID
		e.RejectedName = strings.TrimSpace(req.OperatorName)
		e.RejectedAt = &now
		e.RejectedReason = reason
		e.UpdateTime = now
		return s.m.UpdateTransferTx(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

func (s *Service) assertTransferAvailableTx(ctx context.Context, tx *gorm.DB,
	e *inventorymodel.TransferEntity, items []*inventorymodel.TransferItemEntity) error {
	rows, err := s.m.ListStocksByWarehouseTx(ctx, tx, e.ProjectID, e.FromWarehouseID)
	if err != nil {
		return err
	}
	byID := make(map[string]*inventorymodel.StockEntity, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	for _, it := range items {
		row, ok := byID[it.StockID]
		if !ok {

			return inventoryenums.Err(inventoryenums.ErrItemNotInSourceWarehouse)
		}
		if availableOf(row.Quantity, row.ReservedQuantity) < it.Quantity {
			return inventoryenums.Err(inventoryenums.ErrStockNotEnough)
		}
	}
	return nil
}

func (s *Service) CancelTransfer(ctx context.Context, req *inventorydto.TransferCancelReq) (res *inventorydto.TransferResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.m.LockTransferTx(ctx, tx, projectID, strings.TrimSpace(req.ID))
		if lerr != nil {
			return mapTransferNotFound(lerr)
		}

		if e.Status != TransferStatusPending && e.Status != TransferStatusRequested {
			return inventoryenums.Err(inventoryenums.ErrTransferStatusInvalid)
		}
		e.Status = TransferStatusCancelled
		e.UpdateTime = now
		if note := strings.TrimSpace(req.Remark); note != "" {
			e.Remark = orJoinRemark(e.Remark, note)
		}
		return s.m.UpdateTransferTx(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.transferRespTx(ctx, nil, projectID, req.ID)
}

func (s *Service) ListTransferStockOptions(ctx context.Context, req *inventorydto.TransferStockOptionReq) (
	list []*inventorydto.TransferStockOptionResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	if _, werr := s.resolveWarehouse(ctx, projectID, req.WarehouseID); werr != nil {
		return nil, werr
	}
	rows, err := s.m.ListStocksByWarehouseTx(ctx, nil, projectID, req.WarehouseID)
	if err != nil {
		return nil, err
	}
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	limit := transferStockOptionLimit(req.Limit)
	list = make([]*inventorydto.TransferStockOptionResp, 0, len(rows))
	for _, r := range rows {
		available := availableOf(r.Quantity, r.ReservedQuantity)
		if available <= 0 {
			continue
		}
		if keyword != "" && !strings.Contains(strings.ToLower(r.SKUCode), keyword) &&
			!strings.Contains(strings.ToLower(r.Name), keyword) {
			continue
		}
		variantID := ""
		if r.VariantID != nil {
			variantID = *r.VariantID
		}
		list = append(list, &inventorydto.TransferStockOptionResp{
			StockID: r.ID, SKUCode: r.SKUCode, VariantID: variantID,
			Name:     r.Name,
			Quantity: r.Quantity, ReservedQuantity: r.ReservedQuantity,
			AvailableQuantity: available,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].AvailableQuantity != list[j].AvailableQuantity {
			return list[i].AvailableQuantity > list[j].AvailableQuantity
		}
		return list[i].SKUCode < list[j].SKUCode
	})
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func transferStockOptionLimit(limit int) int {
	if limit <= 0 {
		return defaultTransferStockOptionLimit
	}
	if limit > maxTransferStockOptionLimit {
		return maxTransferStockOptionLimit
	}
	return limit
}

func (s *Service) ListTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (
	list []*inventorydto.TransferRowResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	page, size := transferPageArgs(req.Page, req.Size)
	rows, err := s.m.ListTransfers(ctx, inventorymodel.TransferListFilter{
		ProjectID: projectID, Status: strings.TrimSpace(req.Status),
		Keyword: strings.TrimSpace(req.Keyword),
	}, size, utils.PageOffset(page, size))
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.TransferRowResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toTransferRowResp(r))
	}
	return list, nil
}

func (s *Service) CountTransfers(ctx context.Context, req *inventorydto.ListTransferReq) (n int64, err error) {
	if req == nil {
		return 0, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return 0, perr
	}
	return s.m.CountTransfers(ctx, inventorymodel.TransferListFilter{
		ProjectID: projectID, Status: strings.TrimSpace(req.Status),
		Keyword: strings.TrimSpace(req.Keyword),
	})
}

func (s *Service) GetTransfer(ctx context.Context, req *inventorydto.GetTransferReq) (
	res *inventorydto.TransferDetailResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	e, gerr := s.m.GetTransfer(ctx, strings.TrimSpace(req.ID), projectID)
	if gerr != nil {
		return nil, mapTransferNotFound(gerr)
	}
	base, err := s.transferRespTx(ctx, nil, projectID, e.ID)
	if err != nil {
		return nil, err
	}
	detail := &inventorydto.TransferDetailResp{Transfer: base}
	for _, att := range base.Attachments {
		switch att.UsageType {
		case TransferAttachmentRequest:
			detail.RequestAttachments = append(detail.RequestAttachments, att)
		case TransferAttachmentShip:
			detail.ShipAttachments = append(detail.ShipAttachments, att)
		case TransferAttachmentReceive:
			detail.ReceiveAttachments = append(detail.ReceiveAttachments, att)
		}
	}
	return detail, nil
}

func (s *Service) transferRespTx(ctx context.Context, tx *gorm.DB, projectID, id string) (
	res *inventorydto.TransferResp, err error) {
	e, gerr := s.m.GetTransfer(ctx, id, projectID)
	if gerr != nil {
		return nil, mapTransferNotFound(gerr)
	}
	items, ierr := s.m.ListTransferItems(ctx, projectID, id)
	if ierr != nil {
		return nil, ierr
	}
	atts, aerr := s.m.ListTransferAttachments(ctx, projectID, id)
	if aerr != nil {
		return nil, aerr
	}
	row := &inventorydto.TransferRowResp{
		ID: e.ID, TransferNo: e.TransferNo, Status: e.Status,
		Remark:          e.Remark,
		FromWarehouseID: e.FromWarehouseID, ToWarehouseID: e.ToWarehouseID,
		CreateTime: e.CreateTime.Format(time.RFC3339),
	}

	if wh, werr := s.resolveWarehouse(ctx, projectID, e.FromWarehouseID); werr == nil {
		row.FromCode, row.FromName = wh.Code, wh.Name
	}
	if wh, werr := s.resolveWarehouse(ctx, projectID, e.ToWarehouseID); werr == nil {
		row.ToCode, row.ToName = wh.Code, wh.Name
	}
	if e.ShippedAt != nil {
		row.ShippedAt = e.ShippedAt.Format(time.RFC3339)
	}
	if e.ReceivedAt != nil {
		row.ReceivedAt = e.ReceivedAt.Format(time.RFC3339)
	}
	res = &inventorydto.TransferResp{
		TransferRowResp: *row,
		CreateBy:        e.CreateBy, ShippedBy: e.ShippedBy, ReceivedBy: e.ReceivedBy,
		RequestedBy: e.RequestedBy, ApprovedBy: e.ApprovedBy,
		ApprovedName: e.ApprovedName,
		RejectedBy:   e.RejectedBy, RejectedName: e.RejectedName,
		RejectedReason: e.RejectedReason,
	}
	if e.RequestedAt != nil {
		row.RequestedAt = e.RequestedAt.Format(time.RFC3339)
		res.RequestedAt = row.RequestedAt
	}
	if e.ApprovedAt != nil {
		row.ApprovedAt = e.ApprovedAt.Format(time.RFC3339)
		res.ApprovedAt = row.ApprovedAt
	}
	if e.RejectedAt != nil {
		row.RejectedAt = e.RejectedAt.Format(time.RFC3339)
		res.RejectedAt = row.RejectedAt
	}
	total := 0
	for _, it := range items {
		variantID := ""
		if it.VariantID != nil {
			variantID = *it.VariantID
		}
		res.Items = append(res.Items, &inventorydto.TransferItemResp{
			ID: it.ID, StockID: it.StockID, VariantID: variantID,
			SKUCode: it.SKUCode, Quantity: it.Quantity,
		})
		total += it.Quantity
	}
	res.ItemCount = len(res.Items)
	res.TotalQuantity = total
	for _, att := range atts {
		brief := &inventorydto.TransferAttachmentResp{
			AttachmentID: att.AttachmentID, UsageType: att.UsageType,
		}

		if s.attachments != nil {
			if got, aerr := s.attachments.ResolveAttachment(ctx, projectID, att.AttachmentID); aerr == nil && got != nil {
				brief.FileName, brief.FileSize = got.FileName, got.FileSize
				brief.MimeType, brief.URL = got.MimeType, got.URL
			}
		}
		res.Attachments = append(res.Attachments, brief)
	}
	return res, nil
}

func (s *Service) resolveAttachmentIDs(ctx context.Context, projectID string, ids []int64) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if s.attachments == nil {

			out = append(out, id)
			continue
		}
		if _, err := s.attachments.ResolveAttachment(ctx, projectID, id); err != nil {
			if strings.Contains(err.Error(), "ErrAttachmentNotFound") {
				return nil, inventoryenums.Err(inventoryenums.ErrAttachmentNotFound)
			}
			return nil, err
		}
		out = append(out, id)
	}

	return out, nil
}

func (s *Service) nextTransferNoTx(ctx context.Context, tx *gorm.DB, projectID string, now time.Time) (string, error) {
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	n, err := s.m.CountTransfersOnDayTx(ctx, tx, projectID, dayStart, dayStart.Add(24*time.Hour))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("TRF%s%06d", now.Format("20060102"), n+1), nil
}

func transferPageArgs(page, size int) (outPage, outSize int) {
	outPage, outSize = 1, defaultPageSize
	if page > 0 {
		outPage = page
	}
	if size > 0 {
		outSize = size
	}
	if outSize > maxPageSize {
		outSize = maxPageSize
	}
	return outPage, outSize
}

func toTransferRowResp(r *inventorymodel.TransferListRow) *inventorydto.TransferRowResp {
	if r == nil {
		return nil
	}
	out := &inventorydto.TransferRowResp{
		ID: r.ID, TransferNo: r.TransferNo, Status: r.Status,
		Remark:          r.Remark,
		FromWarehouseID: r.FromWarehouseID, FromCode: r.FromCode, FromName: r.FromName,
		ToWarehouseID: r.ToWarehouseID, ToCode: r.ToCode, ToName: r.ToName,
		ItemCount: r.ItemCount, TotalQuantity: r.TotalQuantity,
		CreateTime: r.CreateTime.Format(time.RFC3339),
	}
	if r.ShippedAt != nil {
		out.ShippedAt = r.ShippedAt.Format(time.RFC3339)
	}
	if r.ReceivedAt != nil {
		out.ReceivedAt = r.ReceivedAt.Format(time.RFC3339)
	}
	if r.RequestedAt != nil {
		out.RequestedAt = r.RequestedAt.Format(time.RFC3339)
	}
	if r.ApprovedAt != nil {
		out.ApprovedAt = r.ApprovedAt.Format(time.RFC3339)
	}
	if r.RejectedAt != nil {
		out.RejectedAt = r.RejectedAt.Format(time.RFC3339)
	}
	out.RejectedReason = r.RejectedReason
	out.ApprovedName = r.ApprovedName
	out.RejectedName = r.RejectedName
	return out
}

func orJoinRemark(existing, extra string) string {
	if strings.TrimSpace(existing) == "" {
		return extra
	}
	return existing + "；" + extra
}

func mapTransferNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return inventoryenums.Err(inventoryenums.ErrTransferNotFound)
	}
	return err
}

var _ inventorycontract.TransferService = (*Service)(nil)
