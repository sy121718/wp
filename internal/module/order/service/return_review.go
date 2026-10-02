package orderservice

// return_review.go — 退货审核与收货（后台侧，BIZ-1）。
//
// 唯一的强顺序：**先入库、后退款**。反过来的失败模式是「钱退了、货没回来」，
// 而这正是退货流程最容易被薅的地方 —— 所以收货把入库放在退款之前，
// 且入库失败时直接返回，绝不进退款。
//
// 两段式（批次4-A 收口；跨模块库存变动走事务透传）：
//   ① 门闩 + 入库 + 逐行登记入库数量，**一个事务**：approved → received 只有跨过这一步的
//      那一次调用会执行入库（重复点击 / 网络重试 / 并发点两次都只有一个能通过 ——
//      库存的 ChangeStock 没有幂等键，这道门闩是唯一的护栏）；库存句柄经 ChangeStockTx
//      传进同一事务，任一步失败整体回滚 —— 状态不会停在 received 而货没入库，
//      因此不再需要「失败把状态退回 approved」的补偿（rollbackReceive 已删）。
//      前置还要校验**订单**状态（BIZ-04）：订单已取消 / 已退款时货早已归还过，这里拒绝收货。
//   ② 退款 + 置 completed，**同一个事务**（BIZ-06）：原先两者各一个事务，退款成功而收尾
//      失败时退货单永远停在 received（重试补退款必报「已退款」）。全额退货走 refundOrderTx
//      把订单转 refunded；部分退货只记流水号，订单状态不动 —— 还有没退的货，
//      把整单标成已退款会让财务对不上账。
//
// 「全额」的判据是**已实际收货入库**的数量（BIZ-01），不是「已申请」：
// 在途申请还没回来，算进去会让只收到一件的订单提前整单退款。

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

	// ② 退款 + ④ 收尾：**同一个事务**（BIZ-06）。
	//
	// 原先两者各开一个事务：退款提交成功、置 completed 的写入失败（或进程中断）时，
	// 退货单停在 received，而重试路径补退款必报「已退款」—— 单子永远收不了尾。
	// 合成一个事务之后，失败就是整体回滚（退货单仍是 received、退款也没发生），
	// 重试是一条干净路径；修复前中断留下的存量单（订单已 refunded、退货单 received）
	// 由 refundReturnTx 的「退款已完成、只补收尾」分支接住。
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
			// 前置校验**订单**状态（BIZ-04）：门闩只看退货单是不够的。
			//
			// 订单在申请之后被取消（库存已全额归还）或被退款时，货其实已经在那边回来过了 ——
			// 这里再入库就是同一批货归还两次；而紧接着的退款必然失败（订单已是终态），
			// 退货单于是停在 received：货加了、钱没退、单子卡死，只能人工处理。
			// 拒绝得早不如拒绝得准：这一次调用什么都不做，退货单留在 approved。
			//
			// 加锁读订单（不是普通读）：它与「取消订单」是两条会同时改库存的路径，
			// 必须串行化。加锁顺序恒为**退货单 → 订单**（本函数先锁退货单，再锁订单；
			// CancelOrder 只锁订单），不会形成环。
			order, oerr := s.orders.LockByIDTx(ctx, tx, e.ProjectID, e.OrderID)
			if oerr != nil {
				return oerr
			}
			if order == nil {
				return errors.New(orderenums.ErrOrderNotFound)
			}
			if !returnableOrderStatuses[order.Status] {
				return errors.New(orderenums.ErrReturnOrderNotReturnable)
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

// receivedByItemTx 该订单各订单项**已经实际收货入库**的数量 —— BIZ-01 / 02 / 04 / 07 共用的那本账。
//
// 传订单项而不是传 id 列表：调用点手上永远已经有 items（要么刚读过、要么正要归还它们），
// 让调用方各自去拼 id 列表，就是在三个地方重复同一段 5 行代码。
//
// 必须用 tx 读：这份账决定「还要归还多少」「是不是全退」—— 挂在 ctx 上会走另一条连接、
// 读到事务外的快照，并发收货时会把「还有没退的货」判成全退。
func (s *Service) receivedByItemTx(ctx context.Context, tx *gorm.DB, projectID string, orderID uint64,
	items []*ordermodel.OrderItemEntity) (map[uint64]int, error) {
	itemIDs := make([]uint64, 0, len(items))
	for _, it := range items {
		itemIDs = append(itemIDs, it.ID)
	}
	return s.returns.SumReceivedQuantityByOrderItemsTx(ctx, tx, projectID, orderID, itemIDs)
}

// restockLinesFor 把「还没归还的」订单项拼成库存归还行。
//
// 归还量 = 订购数量 − **已实际收货入库**的数量（BIZ-02）：部分退货已经把那部分货加回仓库了，
// 取消（或未发货退款）再按原始数量归还一次就是凭空多出库存 —— 净多一件，且不报错。
//
// 零数量的行直接丢掉：库存变动不接受「归还 0 件」，而那样的调用本身也没有业务含义。
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

// refundReturnTx 事务内退款（句柄由调用方给，BIZ-06）：全额 → 把订单推进到 refunded；
// 部分 → 订单状态不动，只在流转链上记一条说明（还有没退的货）。
//
// 读取口径（审计中优先项）：明细与退货账一律**用本事务的 tx 读**（…Tx 方法）——
// 原先这两个读挂在 ctx 上，走的是另一条连接、读事务外的快照，且缺工程作用域
// （order_items / order_return_items 都带 FORCE 策略）。全额 / 部分的判定建立在这份
// 快照上，并发退货时会把「还有没退的货」判成全退。
//
// 「全额」的判据是**已实际收货入库**的数量而不是「已申请」（BIZ-01）：把在途申请算成已退，
// 会出现「只收到 A 就把整单判成已退款」，B 的申请之后收货时退款必报「已退款」、
// 退货单永久停在 received。
func (s *Service) refundReturnTx(ctx context.Context, tx *gorm.DB, rt *ordermodel.ReturnEntity,
	transactionID, operatorType string, operatorID uint64, operatorName string) error {
	order, lerr := s.orders.LockByIDTx(ctx, tx, rt.ProjectID, rt.OrderID)
	if lerr != nil {
		return lerr
	}
	if order == nil {
		return errors.New(orderenums.ErrOrderNotFound)
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
		// 部分退货：订单状态不动（还有没退的货），但流转链上要留一条 ——
		// 财务对账看的是「这单退了多少钱」，而不是只看状态列。
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
		// 退款已经完成、只差收尾 —— 修复前「退款成功但置 completed 失败」留下的存量单。
		// 不重复退款、也不报「已退款」：调用方接着把 completed 补上就把这单救回来了。
		return nil
	}
	// 全额：退款。restock=false —— 货已经由本单的入库步骤归还过（BIZ-07 的「未发货也归还」
	// 是给后台直接退款那条路径的，退货这里再归一次就是把同一批货加两遍）。
	return s.refundOrderTx(ctx, tx, rt.ProjectID, &orderdto.RefundOrderReq{
		OrderID:       rt.OrderID,
		Reason:        "退货入库后退款（" + rt.ReturnNo + "）",
		TransactionID: strings.TrimSpace(transactionID),
		OperatorType:  operatorType,
		OperatorID:    operatorID,
		OperatorName:  operatorName,
	}, false)
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
