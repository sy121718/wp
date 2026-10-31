package orderservice

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/database"
	"go_wp/pkg/rls"
	"go_wp/pkg/utils"
)

const (
	maxReturnItems = 100

	maxReturnQuantity = 100000
)

var returnableOrderStatuses = map[string]bool{
	ordermodel.OrderStatusPaid:      true,
	ordermodel.OrderStatusShipped:   true,
	ordermodel.OrderStatusCompleted: true,
}

func (s *Service) RequestReturn(ctx context.Context, req *orderdto.ReturnRequestReq) (res *orderdto.VisitorReturnResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	if req.OrderID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, orderenums.Err(orderenums.ErrReturnReasonRequired)
	}
	if len(req.Items) == 0 {
		return nil, orderenums.Err(orderenums.ErrReturnItemsRequired)
	}
	if len(req.Items) > maxReturnItems {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if req.UserID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	seen := make(map[uint64]bool, len(req.Items))
	for _, line := range req.Items {
		if seen[line.OrderItemID] {
			return nil, orderenums.Err(orderenums.ErrInvalidParam)
		}
		seen[line.OrderItemID] = true
	}
	projectID := strings.TrimSpace(req.ProjectID)

	if reqID := strings.TrimSpace(req.RequestID); reqID != "" {
		existing, gerr := s.returns.GetByRequestID(ctx, projectID, reqID)
		if gerr != nil {
			return nil, gerr
		}
		if existing != nil {
			if err := checkVisitorReturnReplay(existing, req); err != nil {
				return nil, err
			}
			items, ierr := s.returns.ItemsByReturnID(ctx, existing.ID)
			if ierr != nil {
				return nil, ierr
			}
			if !sameReturnRequest(existing, items, req) {
				return nil, orderenums.Err(orderenums.ErrReturnRequestConflict)
			}
			return toVisitorReturnResp(existing, items), nil
		}
	}

	order, oerr := s.orders.GetByID(ctx, req.OrderID, projectID)
	if oerr != nil {
		return nil, oerr
	}
	if order == nil || order.ProjectID != projectID {

		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}

	if order.UserID == nil || *order.UserID != req.UserID {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	if !returnableOrderStatuses[order.Status] {
		return nil, orderenums.Err(orderenums.ErrReturnOrderNotReturnable)
	}

	orderItems, ierr := s.items.ListByOrderID(ctx, order.ID)
	if ierr != nil {
		return nil, ierr
	}
	if len(orderItems) == 0 {
		return nil, orderenums.Err(orderenums.ErrOrderHasNoItems)
	}
	byID := make(map[uint64]*ordermodel.OrderItemEntity, len(orderItems))
	for _, it := range orderItems {
		byID[it.ID] = it
	}

	returnNo, nerr := s.newReturnNo(ctx, projectID)
	if nerr != nil {
		return nil, nerr
	}
	now := time.Now()
	userID := req.UserID
	head := &ordermodel.ReturnEntity{
		ProjectID:     projectID,
		OrderID:       order.ID,
		OrderNo:       order.OrderNo,
		ReturnNo:      returnNo,
		Status:        ordermodel.ReturnStatusRequested,
		Reason:        reason,
		UserID:        &userID,
		CustomerEmail: order.CustomerEmail,
		CustomerName:  order.CustomerName,
		RequestID:     strings.TrimSpace(req.RequestID),
		CreateTime:    now,
		UpdateTime:    now,
	}
	var items []*ordermodel.ReturnItemEntity
	var returnable map[uint64]int

	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		locked, lerr := s.orders.LockByIDTx(ctx, tx, order.ProjectID, order.ID)
		if lerr != nil {
			return lerr
		}
		if locked == nil || locked.UserID == nil || *locked.UserID != req.UserID {
			return orderenums.Err(orderenums.ErrOrderNotFound)
		}
		if !returnableOrderStatuses[locked.Status] {
			return orderenums.Err(orderenums.ErrReturnOrderNotReturnable)
		}
		returnable, lerr = s.returnableByItemTx(ctx, tx, locked.ProjectID, locked.ID, orderItems)
		if lerr != nil {
			return lerr
		}
		var total int64
		items = make([]*ordermodel.ReturnItemEntity, 0, len(req.Items))
		for _, line := range req.Items {
			it := byID[line.OrderItemID]
			if it == nil {
				return orderenums.Err(orderenums.ErrInvalidParam)
			}
			if line.Quantity <= 0 || line.Quantity > maxReturnQuantity {
				return orderenums.Err(orderenums.ErrReturnQuantityInvalid)
			}
			if line.Quantity > returnable[it.ID] {
				return fmt.Errorf("%w: %s", orderenums.Err(orderenums.ErrReturnQuantityExceeded), it.ProductName)
			}
			amount := lineRefundAmount(it, line.Quantity)
			total += amount
			items = append(items, &ordermodel.ReturnItemEntity{
				OrderItemID:  it.ID,
				ProductID:    it.ProductID,
				VariantID:    it.VariantID,
				ProductName:  it.ProductName,
				VariantLabel: it.VariantLabel,
				SKU:          it.SKU,
				UnitPrice:    it.UnitPrice,
				Quantity:     line.Quantity,
				RefundAmount: amount,
				CreateTime:   now,
			})
		}
		head.RefundAmount = total
		if cerr := s.returns.CreateTx(ctx, tx, head); cerr != nil {
			return cerr
		}
		for _, it := range items {
			it.ReturnID = head.ID
		}
		return s.returns.CreateItemsTx(ctx, tx, items)
	})
	if err != nil {
		if reqID := strings.TrimSpace(req.RequestID); reqID != "" && database.IsUniqueViolation(err) {
			if existing, gerr := s.returns.GetByRequestID(ctx, projectID, reqID); gerr == nil && existing != nil {
				if err := checkVisitorReturnReplay(existing, req); err != nil {
					return nil, err
				}
				items, ierr := s.returns.ItemsByReturnID(ctx, existing.ID)
				if ierr != nil {
					return nil, ierr
				}
				if !sameReturnRequest(existing, items, req) {
					return nil, orderenums.Err(orderenums.ErrReturnRequestConflict)
				}
				return toVisitorReturnResp(existing, items), nil
			}
		}
		return nil, err
	}
	return toVisitorReturnResp(head, items), nil
}

func (s *Service) CancelReturn(ctx context.Context, req *orderdto.ReturnCancelReq) (err error) {
	if req == nil || req.ReturnID == 0 || req.UserID == 0 {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}

	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return perr
	}
	now := time.Now()
	return s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, req.ReturnID)
		if lerr != nil {
			return lerr
		}
		if e == nil || e.UserID == nil || *e.UserID != req.UserID {

			return orderenums.Err(orderenums.ErrReturnNotFound)
		}
		if e.Status != ordermodel.ReturnStatusRequested {

			return orderenums.Err(orderenums.ErrReturnNotCancellable)
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":      ordermodel.ReturnStatusCancelled,
			"admin_note":  strings.TrimSpace(req.Reason),
			"update_time": now,
		})
	})
}

func (s *Service) ListVisitorReturns(ctx context.Context, req *orderdto.VisitorReturnListReq) (res *orderdto.VisitorReturnListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	if req.UserID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if req.OrderID != 0 {
		order, readErr := s.orders.GetByIDForUser(ctx, req.ProjectID, req.OrderID, req.UserID)
		if readErr != nil {
			return nil, readErr
		}
		if order == nil {
			return nil, orderenums.Err(orderenums.ErrOrderNotFound)
		}
	}
	uid := req.UserID
	list, total, lerr := s.returns.List(ctx, ordermodel.ReturnFilter{
		ProjectID: req.ProjectID,
		OrderID:   req.OrderID,
		UserID:    &uid,
		Offset:    req.Offset,
		Limit:     req.Limit,
	})
	if lerr != nil {
		return nil, lerr
	}
	items, ierr := s.returns.ItemsByReturnIDs(ctx, idsOfReturns(list))
	if ierr != nil {
		return nil, ierr
	}
	byReturn := groupReturnItems(items)
	res = &orderdto.VisitorReturnListResp{List: make([]*orderdto.VisitorReturnResp, 0, len(list)), Total: total}
	for _, e := range list {
		res.List = append(res.List, toVisitorReturnResp(e, byReturn[e.ID]))
	}
	return res, nil
}

func (s *Service) returnableByItem(ctx context.Context, projectID string, orderID uint64, items []*ordermodel.OrderItemEntity) (map[uint64]int, error) {
	ids, err := s.returns.IDsByOrder(ctx, projectID, orderID, ordermodel.ReturnActiveStatuses)
	if err != nil {
		return nil, err
	}
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	sums, serr := s.returns.SumQuantityByOrderItems(ctx, ids, itemIDs)
	if serr != nil {
		return nil, serr
	}
	return returnableFromSums(items, sums), nil
}

func (s *Service) returnableByItemTx(ctx context.Context, tx *gorm.DB, projectID string, orderID uint64, items []*ordermodel.OrderItemEntity) (map[uint64]int, error) {
	ids, err := s.returns.IDsByOrderTx(ctx, tx, projectID, orderID, ordermodel.ReturnActiveStatuses)
	if err != nil {
		return nil, err
	}
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	sums, serr := s.returns.SumQuantityByOrderItemsTx(ctx, tx, ids, itemIDs)
	if serr != nil {
		return nil, serr
	}
	return returnableFromSums(items, sums), nil
}

func returnableFromSums(items []*ordermodel.OrderItemEntity, sums map[uint64]int) map[uint64]int {
	out := make(map[uint64]int, len(items))
	for _, it := range items {
		left := it.Quantity - sums[it.ID]
		if left < 0 {
			left = 0
		}
		out[it.ID] = left
	}
	return out
}

func (s *Service) newReturnNo(ctx context.Context, projectID string) (no string, err error) {
	const attempts = 5
	buf := make([]byte, 4)
	for i := 0; i < attempts; i++ {
		if _, rerr := rand.Read(buf); rerr != nil {
			return "", rerr
		}
		no = fmt.Sprintf("RTR%s%08X", time.Now().Format("20060102"),
			uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
		list, _, lerr := s.returns.List(ctx, ordermodel.ReturnFilter{ProjectID: projectID, Keyword: no, Limit: 1})
		if lerr != nil {
			return "", lerr
		}
		if len(list) == 0 {
			return no, nil
		}
	}
	return "", orderenums.Err(orderenums.ErrOrderNoTaken)
}

func idsOfReturns(list []*ordermodel.ReturnEntity) []uint64 {
	ids := make([]uint64, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.ID)
	}
	return ids
}

func groupReturnItems(items []*ordermodel.ReturnItemEntity) map[uint64][]*ordermodel.ReturnItemEntity {
	out := make(map[uint64][]*ordermodel.ReturnItemEntity)
	for _, it := range items {
		out[it.ReturnID] = append(out[it.ReturnID], it)
	}
	return out
}

func toReturnResp(e *ordermodel.ReturnEntity, items []*ordermodel.ReturnItemEntity, returnable map[uint64]int) *orderdto.ReturnResp {
	if e == nil {
		return nil
	}
	res := &orderdto.ReturnResp{
		ID: e.ID, ProjectID: e.ProjectID,
		OrderID: e.OrderID, OrderNo: e.OrderNo, ReturnNo: e.ReturnNo,
		Status:        e.Status,
		StatusTone:    orderenums.ReturnStatusTone(e.Status),
		Reason:        e.Reason,
		RefundAmount:  e.RefundAmount,
		RefundLabel:   yuanText(e.RefundAmount),
		UserID:        e.UserID,
		CustomerEmail: e.CustomerEmail,
		CustomerName:  e.CustomerName,
		AdminNote:     e.AdminNote,
		ReviewerName:  e.ReviewerName,
		ReviewedAt:    utils.NewJSONTimePtr(e.ReviewedAt),
		ReceivedAt:    utils.NewJSONTimePtr(e.ReceivedAt),
		RefundedAt:    utils.NewJSONTimePtr(e.RefundedAt),
		TransactionID: e.TransactionID,
		CreateTime:    utils.NewJSONTime(e.CreateTime),
		UpdateTime:    utils.NewJSONTime(e.UpdateTime),
		Items:         make([]*orderdto.ReturnItemResp, 0, len(items)),
	}
	for _, it := range items {
		line := &orderdto.ReturnItemResp{
			OrderItemID: it.OrderItemID,
			ProductID:   it.ProductID,
			VariantID:   it.VariantID,
			ProductName: it.ProductName, VariantLabel: it.VariantLabel, SKU: it.SKU,
			UnitPrice: it.UnitPrice, UnitPriceLabel: yuanText(it.UnitPrice),
			Quantity: it.Quantity, ReceivedQuantity: it.ReceivedQuantity,
			RefundAmount: it.RefundAmount, RefundLabel: yuanText(it.RefundAmount),
		}
		if returnable != nil {
			line.Returnable = returnable[it.OrderItemID]
		}
		res.Items = append(res.Items, line)
	}

	for _, it := range items {
		if it.ReceivedQuantity < it.Quantity {
			res.PendingInboundLines++
		}
	}
	return res
}

func (s *Service) attachReturnWarehouses(ctx context.Context, projectID string, resps []*orderdto.ReturnResp, itemsByReturn map[uint64][]*ordermodel.ReturnItemEntity) {
	if len(resps) == 0 {
		return
	}
	itemIDs := make([]uint64, 0, len(resps)*2)
	for _, r := range resps {
		for _, it := range itemsByReturn[r.ID] {
			itemIDs = append(itemIDs, it.OrderItemID)
		}
	}
	if len(itemIDs) == 0 {
		return
	}

	byShipment := map[uint64]string{}
	if s.shipments != nil {
		if got, err := s.shipments.WarehouseByOrderItem(ctx, itemIDs); err == nil {
			byShipment = got
		}
	}

	missing := make([]uint64, 0, len(itemIDs))
	for _, id := range itemIDs {
		if byShipment[id] == "" {
			missing = append(missing, id)
		}
	}
	byItem := map[uint64]string{}
	if len(missing) > 0 {
		if got, err := s.items.WarehouseByIDs(ctx, missing); err == nil {
			byItem = got
		}
	}
	names := s.warehouseNames(ctx, projectID)
	for _, r := range resps {
		for _, it := range itemsByReturn[r.ID] {
			whID := byShipment[it.OrderItemID]
			if whID == "" {
				whID = byItem[it.OrderItemID]
			}
			if whID == "" {
				continue
			}
			r.OriginalWarehouseName = names[whID]
			break
		}
	}
}

func yuanText(cents int64) string {
	return centsToYuanLabel(cents) + " 元"
}

func (s *Service) ReturnableOfOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.ReturnableResp, err error) {
	if req == nil || req.OrderID == 0 || req.UserID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}

	order, oerr := s.orders.GetByIDForUser(ctx, req.ProjectID, req.OrderID, req.UserID)
	if oerr != nil {
		return nil, oerr
	}
	if order == nil {
		return nil, orderenums.Err(orderenums.ErrOrderNotFound)
	}
	orderItems, ierr := s.items.ListByOrderID(ctx, order.ID)
	if ierr != nil {
		return nil, ierr
	}
	returnable, rerr := s.returnableByItem(ctx, order.ProjectID, order.ID, orderItems)
	if rerr != nil {
		return nil, rerr
	}
	res = &orderdto.ReturnableResp{Items: make([]*orderdto.ReturnableItem, 0, len(orderItems))}
	for _, it := range orderItems {
		left := returnable[it.ID]
		res.Items = append(res.Items, &orderdto.ReturnableItem{
			OrderItemID: it.ID, Returnable: left, Quantity: it.Quantity,
		})
		res.ReturnableTotal += left
	}
	res.CanRequest = res.ReturnableTotal > 0 && returnableOrderStatuses[order.Status]
	return res, nil
}

func (s *Service) ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (res *orderdto.ReturnListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	list, total, lerr := s.returns.List(ctx, ordermodel.ReturnFilter{
		ProjectID: req.ProjectID,
		Status:    strings.TrimSpace(req.Status),
		Keyword:   req.Keyword,
		OrderID:   req.OrderID,
		Offset:    req.Offset,
		Limit:     req.Limit,
	})
	if lerr != nil {
		return nil, lerr
	}
	counts, cerr := s.returns.CountByStatus(ctx, req.ProjectID)
	if cerr != nil {
		return nil, cerr
	}
	items, ierr := s.returns.ItemsByReturnIDs(ctx, idsOfReturns(list))
	if ierr != nil {
		return nil, ierr
	}
	byReturn := groupReturnItems(items)
	res = &orderdto.ReturnListResp{
		List: make([]*orderdto.ReturnResp, 0, len(list)), Total: total, Counts: counts,
	}
	for _, e := range list {
		res.List = append(res.List, toReturnResp(e, byReturn[e.ID], nil))
	}

	s.attachReturnWarehouses(ctx, req.ProjectID, res.List, byReturn)
	return res, nil
}

func (s *Service) GetReturn(ctx context.Context, returnID uint64) (res *orderdto.ReturnDetailResp, err error) {
	if returnID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}

	projectID, perr := s.locateReturnProject(ctx, returnID)
	if perr != nil {
		return nil, perr
	}
	e, gerr := s.returns.GetByID(ctx, projectID, returnID)
	if gerr != nil {
		return nil, gerr
	}
	if e == nil {
		return nil, orderenums.Err(orderenums.ErrReturnNotFound)
	}
	items, ierr := s.returns.ItemsByReturnID(ctx, e.ID)
	if ierr != nil {
		return nil, ierr
	}
	order, oerr := s.orders.GetByID(ctx, e.OrderID, e.ProjectID)
	if oerr != nil {
		return nil, oerr
	}
	var returnable map[uint64]int
	var orderResp *orderdto.OrderResp
	if order != nil {
		orderResp = toOrderResp(order)
		orderItems, lerr := s.items.ListByOrderID(ctx, order.ID)
		if lerr != nil {
			return nil, lerr
		}
		if returnable, lerr = s.returnableByItem(ctx, order.ProjectID, order.ID, orderItems); lerr != nil {
			return nil, lerr
		}
	}
	head := toReturnResp(e, items, returnable)

	s.attachReturnWarehouses(ctx, e.ProjectID, []*orderdto.ReturnResp{head},
		map[uint64][]*ordermodel.ReturnItemEntity{e.ID: items})
	return &orderdto.ReturnDetailResp{Return: head, Order: orderResp}, nil
}

func (s *Service) ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || req.ReturnID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}

	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		return s.approveReturnTx(ctx, tx, projectID, req, now)
	})
	if err != nil {
		return nil, err
	}

	if !req.AutoReceive {
		return s.returnRespOf(ctx, req.ReturnID)
	}

	return s.ReceiveReturn(ctx, &orderdto.ReturnReceiveReq{
		ReturnID:      req.ReturnID,
		WarehouseID:   req.WarehouseID,
		TransactionID: req.TransactionID,
		Remark:        strings.TrimSpace(req.Remark),
		OperatorType:  req.OperatorType,
		OperatorID:    req.OperatorID,
		OperatorName:  req.OperatorName,
	})
}

func (s *Service) approveReturnTx(ctx context.Context, tx *gorm.DB, projectID string,
	req *orderdto.ReturnReviewReq, now time.Time) error {
	{
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, req.ReturnID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return orderenums.Err(orderenums.ErrReturnNotFound)
		}
		if e.Status != ordermodel.ReturnStatusRequested {
			return orderenums.Err(orderenums.ErrReturnNotReviewable)
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.ReturnStatusApproved,
			"admin_note":    strings.TrimSpace(req.Remark),
			"reviewer_id":   req.OperatorID,
			"reviewer_name": strings.TrimSpace(req.OperatorName),
			"reviewed_at":   now,
			"update_time":   now,
		})
	}
}

func (s *Service) ApproveReturnBatch(ctx context.Context, reqs []*orderdto.ReturnReviewReq) (err error) {
	type located struct {
		projectID string
		req       *orderdto.ReturnReviewReq
	}
	plan := make([]located, 0, len(reqs))
	for _, req := range reqs {
		if req == nil || req.ReturnID == 0 {
			return orderenums.Err(orderenums.ErrInvalidParam)
		}
		projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
		if perr != nil {
			return perr
		}
		req.AutoReceive = false
		plan = append(plan, located{projectID: projectID, req: req})
	}
	now := time.Now()
	return s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		for _, p := range plan {
			if terr := s.approveReturnTx(ctx, tx, p.projectID, p.req, now); terr != nil {
				return terr
			}
		}
		return nil
	})
}

func (s *Service) RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || req.ReturnID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	remark := strings.TrimSpace(req.Remark)
	if remark == "" {

		return nil, orderenums.Err(orderenums.ErrReturnRejectReasonRequired)
	}

	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		return s.rejectReturnTx(ctx, tx, projectID, req, remark, now)
	})
	if err != nil {
		return nil, err
	}
	return s.returnRespOf(ctx, req.ReturnID)
}

func (s *Service) rejectReturnTx(ctx context.Context, tx *gorm.DB, projectID string,
	req *orderdto.ReturnReviewReq, remark string, now time.Time) error {
	{
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, req.ReturnID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return orderenums.Err(orderenums.ErrReturnNotFound)
		}
		if e.Status != ordermodel.ReturnStatusRequested {
			return orderenums.Err(orderenums.ErrReturnNotReviewable)
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.ReturnStatusRejected,
			"admin_note":    remark,
			"reviewer_id":   req.OperatorID,
			"reviewer_name": strings.TrimSpace(req.OperatorName),
			"reviewed_at":   now,
			"update_time":   now,
		})
	}
}

func (s *Service) RejectReturnBatch(ctx context.Context, reqs []*orderdto.ReturnReviewReq) (err error) {
	type located struct {
		projectID string
		req       *orderdto.ReturnReviewReq
		remark    string
	}
	plan := make([]located, 0, len(reqs))
	for _, req := range reqs {
		if req == nil || req.ReturnID == 0 {
			return orderenums.Err(orderenums.ErrInvalidParam)
		}
		remark := strings.TrimSpace(req.Remark)
		if remark == "" {

			return orderenums.Err(orderenums.ErrReturnRejectReasonRequired)
		}
		projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
		if perr != nil {
			return perr
		}
		plan = append(plan, located{projectID: projectID, req: req, remark: remark})
	}
	now := time.Now()
	return s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		for _, p := range plan {
			if terr := s.rejectReturnTx(ctx, tx, p.projectID, p.req, p.remark, now); terr != nil {
				return terr
			}
		}
		return nil
	})
}

func (s *Service) ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || req.ReturnID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}

	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return nil, perr
	}

	rt, admitted, aerr := s.admitReceive(ctx, projectID, req.ReturnID, req.WarehouseID, req.Remark)
	if aerr != nil {
		return nil, aerr
	}
	if !admitted && rt.Status == ordermodel.ReturnStatusCompleted {

		return s.returnRespOf(ctx, rt.ID)
	}

	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.returns.LockByIDTx(ctx, tx, rt.ProjectID, rt.ID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return orderenums.Err(orderenums.ErrReturnNotFound)
		}
		if e.Status == ordermodel.ReturnStatusCompleted {
			return nil
		}
		if rerr := s.refundReturnTx(ctx, tx, e, req.TransactionID, req.OperatorType, req.OperatorID, req.OperatorName); rerr != nil {
			return rerr
		}
		fields := map[string]any{
			"status":      ordermodel.ReturnStatusCompleted,
			"refunded_at": now,
			"update_time": now,
		}
		if tid := strings.TrimSpace(req.TransactionID); tid != "" {
			fields["transaction_id"] = tid
		}
		if note := strings.TrimSpace(req.Remark); note != "" {
			fields["admin_note"] = note
		}
		if uerr := s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
			return uerr
		}

		return s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.OrderID, map[string]any{
			"fulfillment_status": ordermodel.FulfillmentReturnDone,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.returnRespOf(ctx, rt.ID)
}

func (s *Service) admitReceive(ctx context.Context, projectID string, returnID uint64, warehouseID, remark string) (
	rt *ordermodel.ReturnEntity, admitted bool, err error) {
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, returnID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return orderenums.Err(orderenums.ErrReturnNotFound)
		}
		rt = e
		switch e.Status {
		case ordermodel.ReturnStatusApproved:

			order, oerr := s.orders.LockByIDTx(ctx, tx, e.ProjectID, e.OrderID)
			if oerr != nil {
				return oerr
			}
			if order == nil {
				return orderenums.Err(orderenums.ErrOrderNotFound)
			}
			if !returnableOrderStatuses[order.Status] {
				return orderenums.Err(orderenums.ErrReturnOrderNotReturnable)
			}
			admitted = true
			fields := map[string]any{
				"status":      ordermodel.ReturnStatusReceived,
				"received_at": now,
				"update_time": now,
			}
			if note := strings.TrimSpace(remark); note != "" {
				fields["admin_note"] = note
			}
			if uerr := s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
				return uerr
			}

			items, ierr := s.returns.ItemsByReturnIDTx(ctx, tx, e.ID)
			if ierr != nil {
				return ierr
			}
			if len(items) == 0 {
				return orderenums.Err(orderenums.ErrReturnItemsRequired)
			}

			if serr := s.stockInReturnTx(ctx, tx, e, items, warehouseID); serr != nil {
				return serr
			}

			for _, it := range items {
				if uerr := s.returns.UpdateItemReceivedTx(ctx, tx, it.ID, it.Quantity,
					it.UnitPrice*int64(it.Quantity)); uerr != nil {
					return uerr
				}
			}

			if uerr := s.orders.UpdateFieldsTx(ctx, tx, order.ProjectID, order.ID, map[string]any{
				"fulfillment_status": ordermodel.FulfillmentReturned,
			}); uerr != nil {
				return uerr
			}
			return nil
		case ordermodel.ReturnStatusReceived, ordermodel.ReturnStatusCompleted:

			return nil
		default:
			return orderenums.Err(orderenums.ErrReturnNotReceivable)
		}
	})
	if err != nil {
		return nil, false, err
	}
	return rt, admitted, nil
}

func (s *Service) stockInReturnTx(ctx context.Context, tx *gorm.DB, rt *ordermodel.ReturnEntity,
	items []*ordermodel.ReturnItemEntity, warehouseID string) error {
	wh := strings.TrimSpace(warehouseID)
	lines := make([]ordercontract.StockLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, ordercontract.StockLine{
			ProductID:   it.ProductID,
			VariantID:   it.VariantID,
			SKUCode:     it.SKU,
			Quantity:    it.Quantity,
			WarehouseID: wh,
		})
	}

	shipped, serr := s.orderShippedTx(ctx, tx, rt.OrderID)
	if serr != nil {
		return serr
	}
	if !shipped {

		if rerr := s.stock.ReleaseStockTx(ctx, tx, &ordercontract.StockReservation{
			ProjectID:  rt.ProjectID,
			SourceType: "order_return",
			SourceRef:  rt.ReturnNo,
			Remark:     "未发货退货释放预占，订单 " + rt.OrderNo,
			Lines:      s.returnReservationLinesTx(ctx, tx, rt.OrderID, items),
		}); rerr != nil && !strings.Contains(rerr.Error(), "ErrStockNotFound") {

		}
		return nil
	}
	if err := s.stock.ChangeStockTx(ctx, tx, &ordercontract.StockAdjustment{
		ProjectID:  rt.ProjectID,
		ReasonCode: "return_in",
		SourceType: "order_return",
		SourceRef:  rt.ReturnNo,
		Remark:     "退货入库，订单 " + rt.OrderNo,
		Lines:      lines,
	}); err != nil {
		return orderenums.Err(orderenums.ErrStockUnavailable)
	}

	_ = s.stock.ReleaseStockTx(ctx, tx, &ordercontract.StockReservation{
		ProjectID:  rt.ProjectID,
		SourceType: "order_return",
		SourceRef:  rt.ReturnNo,
		Remark:     "退货入库释放预占，订单 " + rt.OrderNo,
		Lines:      lines,
	})
	return nil
}

func (s *Service) returnReservationLinesTx(ctx context.Context, tx *gorm.DB, orderID uint64,
	returnItems []*ordermodel.ReturnItemEntity) []ordercontract.StockLine {
	items, err := s.items.ListByOrderIDTx(ctx, tx, orderID)
	if err != nil {
		return nil
	}
	warehouseOf := make(map[uint64]*string, len(items))
	for _, it := range items {
		warehouseOf[it.ID] = it.WarehouseID
	}
	out := make([]ordercontract.StockLine, 0, len(returnItems))
	for _, rit := range returnItems {
		wh := warehouseOf[rit.OrderItemID]
		if wh == nil || strings.TrimSpace(*wh) == "" {
			continue
		}
		out = append(out, ordercontract.StockLine{
			ProductID: rit.ProductID, VariantID: rit.VariantID, SKUCode: rit.SKU,
			Quantity: rit.Quantity, WarehouseID: *wh,
		})
	}
	return out
}

func (s *Service) orderShippedTx(ctx context.Context, tx *gorm.DB, orderID uint64) (bool, error) {
	if s.shipments == nil {

		return true, nil
	}
	list, err := s.shipments.ListByOrderTx(ctx, tx, orderID)
	if err != nil {
		return false, err
	}
	return len(list) > 0, nil
}

func (s *Service) receivedByItemTx(ctx context.Context, tx *gorm.DB, projectID string, orderID uint64,
	items []*ordermodel.OrderItemEntity) (map[uint64]int, error) {
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	return s.returns.SumReceivedQuantityByOrderItemsTx(ctx, tx, projectID, orderID, itemIDs)
}

func restockLinesFor(items []*ordermodel.OrderItemEntity, received map[uint64]int) []ordercontract.StockLine {
	lines := make([]ordercontract.StockLine, 0, len(items))
	for _, it := range items {
		left := it.Quantity - received[it.ID]
		if left <= 0 {
			continue
		}
		lines = append(lines, ordercontract.StockLine{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			SKUCode:   it.SKU,
			Quantity:  left,
		})
	}
	return lines
}

func (s *Service) refundReturnTx(ctx context.Context, tx *gorm.DB, rt *ordermodel.ReturnEntity,
	transactionID, operatorType string, operatorID uint64, operatorName string) error {
	order, lerr := s.orders.LockByIDTx(ctx, tx, rt.ProjectID, rt.OrderID)
	if lerr != nil {
		return lerr
	}
	if order == nil {
		return orderenums.Err(orderenums.ErrOrderNotFound)
	}
	orderItems, ierr := s.items.ListByOrderIDTx(ctx, tx, rt.OrderID)
	if ierr != nil {
		return ierr
	}
	received, rerr := s.receivedByItemTx(ctx, tx, rt.ProjectID, rt.OrderID, orderItems)
	if rerr != nil {
		return rerr
	}
	full := len(orderItems) > 0
	for _, it := range orderItems {
		if received[it.ID] < it.Quantity {
			full = false
			break
		}
	}

	if !full {

		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      order.ID,
			FromStatus:   order.Status,
			ToStatus:     order.Status,
			OperatorType: defaultString(operatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   operatorID,
			OperatorName: strings.TrimSpace(operatorName),
			Remark:       fmt.Sprintf("部分退货退款 %s（%s）", yuanText(rt.RefundAmount), rt.ReturnNo),
			CreateTime:   time.Now(),
		})
	}

	if order.Status == ordermodel.OrderStatusRefunded {

		return nil
	}

	return s.refundOrderTx(ctx, tx, rt.ProjectID, &orderdto.RefundOrderReq{
		OrderID:       rt.OrderID,
		Reason:        "退货入库后退款（" + rt.ReturnNo + "）",
		TransactionID: strings.TrimSpace(transactionID),
		OperatorType:  operatorType,
		OperatorID:    operatorID,
		OperatorName:  operatorName,
	}, false)
}

func (s *Service) returnRespOf(ctx context.Context, returnID uint64) (*orderdto.ReturnResp, error) {
	projectID, perr := s.locateReturnProject(ctx, returnID)
	if perr != nil {
		return nil, perr
	}
	e, err := s.returns.GetByID(ctx, projectID, returnID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, orderenums.Err(orderenums.ErrReturnNotFound)
	}
	items, ierr := s.returns.ItemsByReturnID(ctx, e.ID)
	if ierr != nil {
		return nil, ierr
	}
	return toReturnResp(e, items, nil), nil
}

func checkVisitorReturnReplay(existing *ordermodel.ReturnEntity, req *orderdto.ReturnRequestReq) error {
	if existing.UserID == nil || *existing.UserID != req.UserID || existing.OrderID != req.OrderID {
		return orderenums.Err(orderenums.ErrOrderNotFound)
	}
	return nil
}

func toVisitorReturnResp(e *ordermodel.ReturnEntity, items []*ordermodel.ReturnItemEntity) *orderdto.VisitorReturnResp {
	r := &orderdto.VisitorReturnResp{ID: e.ID, OrderID: e.OrderID, OrderNo: e.OrderNo, ReturnNo: e.ReturnNo, Status: e.Status, Reason: e.Reason, RefundAmount: e.RefundAmount,
		Items: make([]*orderdto.VisitorReturnItemResp, 0, len(items)), ReviewedAt: utils.NewJSONTimePtr(e.ReviewedAt), ReceivedAt: utils.NewJSONTimePtr(e.ReceivedAt), RefundedAt: utils.NewJSONTimePtr(e.RefundedAt), CreateTime: utils.NewJSONTime(e.CreateTime)}
	for _, it := range items {
		r.Items = append(r.Items, &orderdto.VisitorReturnItemResp{OrderItemID: it.OrderItemID, ProductName: it.ProductName, VariantLabel: it.VariantLabel, SKU: it.SKU, Quantity: it.Quantity, ReceivedQuantity: it.ReceivedQuantity, UnitPrice: it.UnitPrice, RefundAmount: it.RefundAmount})
	}
	return r
}

func sameReturnRequest(existing *ordermodel.ReturnEntity, items []*ordermodel.ReturnItemEntity, req *orderdto.ReturnRequestReq) bool {
	if existing.Reason != strings.TrimSpace(req.Reason) || len(items) != len(req.Items) {
		return false
	}
	quantities := make(map[uint64]int, len(items))
	for _, item := range items {
		quantities[item.OrderItemID] = item.Quantity
	}
	for _, item := range req.Items {
		if quantities[item.OrderItemID] != item.Quantity {
			return false
		}
	}
	return true
}
