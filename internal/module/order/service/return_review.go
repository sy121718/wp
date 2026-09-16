package orderservice

// return_review.go — 退货审核与收货（后台侧，BIZ-1）。
//
// 唯一的强顺序：**先入库、后退款**。反过来的失败模式是「钱退了、货没回来」，
// 而这正是退货流程最容易被薅的地方 —— 所以收货把入库放在退款之前，
// 且入库失败时直接返回，绝不进退款。
//
// 三段式（与 CancelOrder 同源：先落账、后动库存、失败留痕）：
//   ① 门闩（事务）：approved → received。**只有跨过这一步的那一次调用会执行入库**，
//      重复点击 / 网络重试 / 并发点两次都只有一个能通过 ——
//      库存的 ChangeStock 没有幂等键，这道门闩是唯一的护栏。
//   ② 入库（事务外，跨模块）：失败则把状态退回 approved 并留痕，等人工重试。
//   ③ 退款（事务外）：全额退货走 RefundOrder（订单转 refunded）；部分退货只记流水号，
//      订单状态不动 —— 还有没退的货，把整单标成已退款会让财务对不上账。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// ReturnableOfOrder 该订单各订单项的当前可退数量（访客侧）。
func (s *Service) ReturnableOfOrder(ctx context.Context, req *orderdto.VisitorOrderDetailReq) (res *orderdto.ReturnableResp, err error) {
	if req == nil || req.OrderID == 0 || req.UserID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	// 与访客查详情同一套归属校验：别人的单在这里也同样表现为「订单不存在」。
	order, oerr := s.orders.GetByIDForUser(ctx, req.ProjectID, req.OrderID, req.UserID)
	if oerr != nil {
		return nil, oerr
	}
	if order == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	orderItems, ierr := s.items.ListByOrderID(ctx, order.ID)
	if ierr != nil {
		return nil, ierr
	}
	returnable, rerr := s.returnableByItem(ctx, order.ID, orderItems)
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
	return res, nil
}

// ListReturns 后台退货申请列表（含各状态计数）。
func (s *Service) ListReturns(ctx context.Context, req *orderdto.ReturnListReq) (res *orderdto.ReturnListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
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
	return res, nil
}

// GetReturn 退货单详情（含订单摘要与逐行可退数量）。
func (s *Service) GetReturn(ctx context.Context, returnID uint64) (res *orderdto.ReturnDetailResp, err error) {
	if returnID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	e, gerr := s.returns.GetByID(ctx, "", returnID)
	if gerr != nil {
		return nil, gerr
	}
	if e == nil {
		return nil, errors.New(orderenums.ErrReturnNotFound)
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
		if returnable, lerr = s.returnableByItem(ctx, order.ID, orderItems); lerr != nil {
			return nil, lerr
		}
	}
	return &orderdto.ReturnDetailResp{Return: toReturnResp(e, items, returnable), Order: orderResp}, nil
}

// ApproveReturn 同意退货申请（AutoReceive=true 时一步完成入库 + 退款）。
func (s *Service) ApproveReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || req.ReturnID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.returns.LockByIDTx(ctx, tx, "", req.ReturnID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrReturnNotFound)
		}
		if e.Status != ordermodel.ReturnStatusRequested {
			return errors.New(orderenums.ErrReturnNotReviewable)
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.ReturnStatusApproved,
			"admin_note":    strings.TrimSpace(req.Remark),
			"reviewer_id":   req.OperatorID,
			"reviewer_name": strings.TrimSpace(req.OperatorName),
			"reviewed_at":   now,
			"update_time":   now,
		})
	})
	if err != nil {
		return nil, err
	}
	if !req.AutoReceive {
		return s.returnRespOf(ctx, req.ReturnID)
	}
	// 一步到底：现实里货往往早就到了（客户先联系客服、客服再走系统）。
	// 这里不再开事务嵌套 —— ReceiveReturn 自带三段式与幂等。
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

// RejectReturn 拒绝退货申请（必须给理由）。
func (s *Service) RejectReturn(ctx context.Context, req *orderdto.ReturnReviewReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || req.ReturnID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	remark := strings.TrimSpace(req.Remark)
	if remark == "" {
		// 没有理由的拒绝，客户只会再申请一次 —— 那对双方都是浪费。
		return nil, errors.New(orderenums.ErrReturnRejectReasonRequired)
	}
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.returns.LockByIDTx(ctx, tx, "", req.ReturnID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrReturnNotFound)
		}
		if e.Status != ordermodel.ReturnStatusRequested {
			return errors.New(orderenums.ErrReturnNotReviewable)
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.ReturnStatusRejected,
			"admin_note":    remark,
			"reviewer_id":   req.OperatorID,
			"reviewer_name": strings.TrimSpace(req.OperatorName),
			"reviewed_at":   now,
			"update_time":   now,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.returnRespOf(ctx, req.ReturnID)
}

// ReceiveReturn 确认收货：入库 + 退款。
func (s *Service) ReceiveReturn(ctx context.Context, req *orderdto.ReturnReceiveReq) (res *orderdto.ReturnResp, err error) {
	if req == nil || req.ReturnID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	rt, admitted, aerr := s.admitReceive(ctx, req.ReturnID, req.Remark)
	if aerr != nil {
		return nil, aerr
	}
	if rt.Status == ordermodel.ReturnStatusCompleted {
		// 已完成：幂等返回，不重复做任何事（重复点击与通道重发都会走到这里）。
		return s.returnRespOf(ctx, rt.ID)
	}

	items, ierr := s.returns.ItemsByReturnID(ctx, rt.ID)
	if ierr != nil {
		return nil, ierr
	}
	if len(items) == 0 {
		return nil, errors.New(orderenums.ErrReturnItemsRequired)
	}

	if admitted {
		// ② 入库（事务外，跨模块）。失败则退回 approved 并留痕：状态不能停在
		// 「已收货」而货其实没入库 —— 那会让下一个操作员以为货已经回来了。
		if serr := s.stockInReturn(ctx, rt, items, req.WarehouseID); serr != nil {
			_ = s.rollbackReceive(ctx, rt.ID, serr.Error())
			return nil, serr
		}
	}

	// ③ 退款。失败停在 received（货已入库），可重试 —— 重试时门闩不再放行，
	// 所以不会二次入库，只会补做退款。
	if rerr := s.refundReturn(ctx, rt, req.TransactionID, req.OperatorType, req.OperatorID, req.OperatorName); rerr != nil {
		return nil, rerr
	}

	// ④ 收尾：置 completed 并记流水号。
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.returns.LockByIDTx(ctx, tx, rt.ProjectID, rt.ID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrReturnNotFound)
		}
		if e.Status == ordermodel.ReturnStatusCompleted {
			return nil
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
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields)
	})
	if err != nil {
		return nil, err
	}
	return s.returnRespOf(ctx, rt.ID)
}

// admitReceive 门闩：把 approved 推进到 received，并回答「本次是否由我负责入库」。
//
// 返回的 admitted 为真表示**只有这次调用**会执行入库；already 状态（received / completed）
// 返回 false，调用方据此跳过入库、只补退款。
func (s *Service) admitReceive(ctx context.Context, returnID uint64, remark string) (rt *ordermodel.ReturnEntity, admitted bool, err error) {
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.returns.LockByIDTx(ctx, tx, "", returnID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrReturnNotFound)
		}
		rt = e
		switch e.Status {
		case ordermodel.ReturnStatusApproved:
			admitted = true
			fields := map[string]any{
				"status":      ordermodel.ReturnStatusReceived,
				"received_at": now,
				"update_time": now,
			}
			if note := strings.TrimSpace(remark); note != "" {
				fields["admin_note"] = note
			}
			return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields)
		case ordermodel.ReturnStatusReceived, ordermodel.ReturnStatusCompleted:
			// 已经越过门闩：这批货的入库已经授过权（发生过或正在进行），本次不重复入库。
			return nil
		default:
			return errors.New(orderenums.ErrReturnNotReceivable)
		}
	})
	return rt, admitted, err
}

// stockInReturn 退货入库（走库存变动契约，原因字典 return_in）。
func (s *Service) stockInReturn(ctx context.Context, rt *ordermodel.ReturnEntity, items []*ordermodel.ReturnItemEntity, warehouseID string) error {
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
	if err := s.stock.ChangeStock(ctx, &ordercontract.StockAdjustment{
		ProjectID:  rt.ProjectID,
		ReasonCode: "return_in",
		SourceType: "order_return",
		SourceRef:  rt.ReturnNo,
		Remark:     "退货入库，订单 " + rt.OrderNo,
		Lines:      lines,
	}); err != nil {
		// 入库失败基本都是库存服务不可用（它不是「不足」——入库不会被库存挡住）。
		return errors.New(orderenums.ErrStockUnavailable)
	}
	// 登记每行的实际入库数量（首版一次收齐）。失败向上返回以便重试补写。
	return s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		for _, it := range items {
			if uerr := s.returns.UpdateItemReceivedTx(ctx, tx, it.ID, it.Quantity, it.UnitPrice*int64(it.Quantity)); uerr != nil {
				return uerr
			}
		}
		return nil
	})
}

// rollbackReceive 入库失败后的补偿：状态退回 approved 并留痕。
//
// 补偿本身的失败不再向上冒（调用方已经要拿到入库失败的结论了）：状态停在 received
// 需要人工处理，但**留痕**必须尽力写成 —— 否则下一个操作员会以为货已经入库。
func (s *Service) rollbackReceive(ctx context.Context, returnID uint64, cause string) error {
	return s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.returns.LockByIDTx(ctx, tx, "", returnID)
		if lerr != nil || e == nil {
			return lerr
		}
		if e.Status != ordermodel.ReturnStatusReceived {
			return nil
		}
		return s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":      ordermodel.ReturnStatusApproved,
			"received_at": nil,
			"admin_note":  "入库失败，需人工处理：" + cause,
			"update_time": time.Now(),
		})
	})
}

// refundReturn 退款。返回是否为**全额**退货。
//
// 全额 → 走 RefundOrder 把订单推进到 refunded（它自带行锁与幂等）；
// 部分 → 订单状态不动，只在流转链上记一条说明（还有没退的货）。
func (s *Service) refundReturn(ctx context.Context, rt *ordermodel.ReturnEntity, transactionID, operatorType string, operatorID uint64, operatorName string) error {
	var orderID uint64
	var orderStatus string
	var full bool
	err := s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		order, lerr := s.orders.LockByIDTx(ctx, tx, rt.ProjectID, rt.OrderID)
		if lerr != nil {
			return lerr
		}
		if order == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		orderID = order.ID
		orderStatus = order.Status
		orderItems, ierr := s.items.ListByOrderID(ctx, rt.OrderID)
		if ierr != nil {
			return ierr
		}
		ids, derr := s.returns.IDsByOrder(ctx, rt.OrderID, ordermodel.ReturnActiveStatuses)
		if derr != nil {
			return derr
		}
		itemIDs := make([]uint64, 0, len(orderItems))
		for _, it := range orderItems {
			itemIDs = append(itemIDs, it.ID)
		}
		sums, serr := s.returns.SumQuantityByOrderItems(ctx, ids, itemIDs)
		if serr != nil {
			return serr
		}
		full = true
		for _, it := range orderItems {
			if sums[it.ID] < it.Quantity {
				full = false
				break
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if full {
		return s.RefundOrder(ctx, &orderdto.RefundOrderReq{
			OrderID:       orderID,
			Reason:        "退货入库后退款（" + rt.ReturnNo + "）",
			TransactionID: strings.TrimSpace(transactionID),
			OperatorType:  operatorType,
			OperatorID:    operatorID,
			OperatorName:  operatorName,
		})
	}
	// 部分退货：订单状态不动（还有没退的货），但流转链上要留一条 ——
	// 财务对账看的是「这单退了多少钱」，而不是只看状态列。
	return s.logs.Transaction(ctx, func(tx *gorm.DB) error {
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      orderID,
			FromStatus:   orderStatus,
			ToStatus:     orderStatus,
			OperatorType: defaultString(operatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   operatorID,
			OperatorName: strings.TrimSpace(operatorName),
			Remark:       fmt.Sprintf("部分退货退款 %s（%s）", yuanText(rt.RefundAmount), rt.ReturnNo),
			CreateTime:   time.Now(),
		})
	})
}

// returnRespOf 取一张退货单的视图（含明细）。
func (s *Service) returnRespOf(ctx context.Context, returnID uint64) (*orderdto.ReturnResp, error) {
	e, err := s.returns.GetByID(ctx, "", returnID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New(orderenums.ErrReturnNotFound)
	}
	items, ierr := s.returns.ItemsByReturnID(ctx, e.ID)
	if ierr != nil {
		return nil, ierr
	}
	return toReturnResp(e, items, nil), nil
}
