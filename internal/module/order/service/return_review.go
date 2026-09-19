package orderservice

// return_review.go — 退货审核与收货（后台侧，BIZ-1）。
//
// 唯一的强顺序：**先入库、后退款**。反过来的失败模式是「钱退了、货没回来」，
// 而这正是退货流程最容易被薅的地方 —— 所以收货把入库放在退款之前，
// 且入库失败时直接返回，绝不进退款。
//
// 三段式（2026-09-19 事务收口后重写；跨模块库存变动改为事务透传）：
//   ① 门闩 + 入库 + 逐行登记入库数量，**一个事务**：approved → received 只有跨过这一步的
//      那一次调用会执行入库（重复点击 / 网络重试 / 并发点两次都只有一个能通过 ——
//      库存的 ChangeStock 没有幂等键，这道门闩是唯一的护栏）；库存句柄经 ChangeStockTx
//      传进同一事务，任一步失败整体回滚 —— 状态不会停在 received 而货没入库，
//      因此不再需要「失败把状态退回 approved」的补偿（rollbackReceive 已删）。
//   ② 退款（事务外）：全额退货走 RefundOrder（订单转 refunded）；部分退货只记流水号，
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
	"go_wp/pkg/rls"
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
	// 定位跳（DB-009 第四批）：详情入口只给退货单 id，order_returns 带 FORCE 策略。
	projectID, perr := s.locateReturnProject(ctx, returnID)
	if perr != nil {
		return nil, perr
	}
	e, gerr := s.returns.GetByID(ctx, projectID, returnID)
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
		if returnable, lerr = s.returnableByItem(ctx, order.ProjectID, order.ID, orderItems); lerr != nil {
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
	// 定位跳（DB-009 第五批）：审核入口只给退货单 id。定位必须在事务**外** ——
	// rls.InProjectScope 会另开事务、另取连接，放进已开的事务里既看不到未提交数据，
	// 又可能自锁（见 pkg/rls.ScopeTx 的说明）。
	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, req.ReturnID)
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
	// 定位跳（DB-009 第五批）：同 ApproveReturn —— 定位在事务外，作用域设在事务内。
	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now()
	err = s.returns.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.returns.LockByIDTx(ctx, tx, projectID, req.ReturnID)
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
	// 定位跳（DB-009 第四批）：入库裁决只给退货单 id；定位在事务**外** ——
	// rls.InProjectScope 会另开事务、另取连接，放进已开的事务里既看不到未提交数据，
	// 又可能自锁（见 pkg/rls.ScopeTx 的说明）。
	projectID, perr := s.locateReturnProject(ctx, req.ReturnID)
	if perr != nil {
		return nil, perr
	}
	// ① 门闩 + 入库 + 明细登记：一个事务（失败整体回滚，不留半截收货状态）。
	rt, admitted, aerr := s.admitReceive(ctx, projectID, req.ReturnID, req.WarehouseID, req.Remark)
	if aerr != nil {
		return nil, aerr
	}
	if !admitted && rt.Status == ordermodel.ReturnStatusCompleted {
		// 已完成：幂等返回，不重复做任何事（重复点击与通道重发都会走到这里）。
		return s.returnRespOf(ctx, rt.ID)
	}

	// ② 退款。失败停在 received（货已入库），可重试 —— 重试时门闩不再放行，
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

// admitReceive 门闩 + 入库 + 明细登记（**一个事务**）：把 approved 推进到 received，
// 并在同一事务里完成库存变动与逐行入库数量登记，回答「本次是否由我负责入库」。
//
// 返回的 admitted 为真表示**只有这次调用**会执行入库；already 状态（received / completed）
// 返回 false，调用方据此跳过入库、只补退款。
//
// 三处写入必须同事务（状态 / 库存真源 / 退货明细的 received_quantity）：原先库存变动在
// 事务之外，失败只能靠 rollbackReceive 把状态退回 approved 并留痕 —— 而补偿本身失败时，
// 下一个操作员会以为货已经回来了（这是本次收口要消灭的半截状态）。
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
			if uerr := s.returns.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
				return uerr
			}
			// 明细用 tx 读：非 Tx 读取走另一条连接，读到的是事务外的快照，
			// 且缺工程作用域时在非超级角色下静默返回空集。
			items, ierr := s.returns.ItemsByReturnIDTx(ctx, tx, e.ID)
			if ierr != nil {
				return ierr
			}
			if len(items) == 0 {
				return errors.New(orderenums.ErrReturnItemsRequired)
			}
			// 入库（同库跨模块，句柄透传）：失败 → 整个事务回滚，状态不会停在 received。
			if serr := s.stockInReturnTx(ctx, tx, e, items, warehouseID); serr != nil {
				return serr
			}
			// 逐行登记实际入库数量（首版一次收齐）：与库存变动同一事务，
			// 不会出现「库存加了、退货明细没记」。
			for _, it := range items {
				if uerr := s.returns.UpdateItemReceivedTx(ctx, tx, it.ID, it.Quantity,
					it.UnitPrice*int64(it.Quantity)); uerr != nil {
					return uerr
				}
			}
			return nil
		case ordermodel.ReturnStatusReceived, ordermodel.ReturnStatusCompleted:
			// 已经越过门闩：这批货的入库已经授过权（发生过或正在进行），本次不重复入库。
			return nil
		default:
			return errors.New(orderenums.ErrReturnNotReceivable)
		}
	})
	if err != nil {
		return nil, false, err
	}
	return rt, admitted, nil
}

// stockInReturnTx 退货入库（走库存变动契约的**事务透传**版，原因字典 return_in）。
//
// 入库失败基本都是库存服务不可用（它不是「不足」——入库不会被库存挡住），统一映射成
// ErrStockUnavailable；事务由调用方回滚，所以这里不需要任何补偿动作。
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
	if err := s.stock.ChangeStockTx(ctx, tx, &ordercontract.StockAdjustment{
		ProjectID:  rt.ProjectID,
		ReasonCode: "return_in",
		SourceType: "order_return",
		SourceRef:  rt.ReturnNo,
		Remark:     "退货入库，订单 " + rt.OrderNo,
		Lines:      lines,
	}); err != nil {
		return errors.New(orderenums.ErrStockUnavailable)
	}
	return nil
}

// refundReturn 退款。返回是否为**全额**退货。
//
// 全额 → 走 RefundOrder 把订单推进到 refunded（它自带行锁与幂等）；
// 部分 → 订单状态不动，只在流转链上记一条说明（还有没退的货）。
//
// 读取口径（审计中优先项）：明细与退货汇总一律**用本事务的 tx 读**（…Tx 方法）——
// 原先这两个读挂在 ctx 上，走的是另一条连接、读事务外的快照，且缺工程作用域
// （order_items / order_return_items 都带 FORCE 策略）。全额 / 部分的判定建立在这份
// 快照上，并发退货时会把「还有没退的货」判成全退。这里不为了压缩事务而保留 ctx 读：
// 读的量级是「一张单的订单项 + 该单的退货申请行」，锁范围只有订单头那一行。
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
		orderItems, ierr := s.items.ListByOrderIDTx(ctx, tx, rt.OrderID)
		if ierr != nil {
			return ierr
		}
		ids, derr := s.returns.IDsByOrderTx(ctx, tx, rt.ProjectID, rt.OrderID, ordermodel.ReturnActiveStatuses)
		if derr != nil {
			return derr
		}
		itemIDs := make([]uint64, 0, len(orderItems))
		for _, it := range orderItems {
			itemIDs = append(itemIDs, it.ID)
		}
		sums, serr := s.returns.SumQuantityByOrderItemsTx(ctx, tx, ids, itemIDs)
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
	projectID, perr := s.locateReturnProject(ctx, returnID)
	if perr != nil {
		return nil, perr
	}
	e, err := s.returns.GetByID(ctx, projectID, returnID)
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
