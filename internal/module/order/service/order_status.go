package orderservice

// order_status.go — 订单状态机与流转（BIZ-1 销售侧）。
//
// 状态机用**表驱动**而不是一串 if：全部合法边能一眼看全，加状态时不会漏改某处判断。
// 漏掉的判断不会报错，只会让某条路径永远走不通、或者误放行一条不该有的边 ——
// 两种都是事故。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
)

// allowedTransitions 合法流转边。终态（cancelled / refunded）无出边。
//
//	pending → paid / cancelled
//	paid    → shipped / cancelled / refunded
//	shipped → completed / refunded
//	completed → refunded
//
// 已完成的订单不能取消是有意的：货已经交付，要退只能走退款（钱与货分开处理）。
var allowedTransitions = map[string]map[string]bool{
	ordermodel.OrderStatusPending: {
		ordermodel.OrderStatusPaid:      true,
		ordermodel.OrderStatusCancelled: true,
	},
	ordermodel.OrderStatusPaid: {
		ordermodel.OrderStatusShipped:   true,
		ordermodel.OrderStatusCancelled: true,
		ordermodel.OrderStatusRefunded:  true,
	},
	ordermodel.OrderStatusShipped: {
		ordermodel.OrderStatusCompleted: true,
		ordermodel.OrderStatusRefunded:  true,
	},
	ordermodel.OrderStatusCompleted: {
		ordermodel.OrderStatusRefunded: true,
	},
	ordermodel.OrderStatusCancelled: {},
	ordermodel.OrderStatusRefunded:  {},
}

// isKnownStatus 取值是否在状态集合内。
func isKnownStatus(s string) bool {
	for _, v := range []string{
		ordermodel.OrderStatusPending, ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped,
		ordermodel.OrderStatusCompleted, ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded,
	} {
		if s == v {
			return true
		}
	}
	return false
}

// canTransition 判断一条流转边是否合法。
func canTransition(from, to string) bool {
	edges, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return edges[to]
}

// ChangeStatus 状态流转：行锁读单 → 校验边 → 更新状态与时间戳 → 记流水，全在一个事务里。
//
// 行锁是必须的：并发的两次「发货」若都读到 paid，就会都判定合法，
// 结果是两条流转记录 + 可能两次库存动作。
func (s *Service) ChangeStatus(ctx context.Context, req *orderdto.ChangeStatusReq) (err error) {
	if req == nil || req.OrderID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	to := strings.TrimSpace(req.ToStatus)
	if !isKnownStatus(to) {
		return errors.New(orderenums.ErrStatusInvalid)
	}
	// 取消与退款有专门的用例（它们各有额外语义），这里拒绝走通用路径，
	// 否则「取消」会绕过归还库存、「退款」会绕过流水号记录。
	if to == ordermodel.OrderStatusCancelled || to == ordermodel.OrderStatusRefunded {
		return errors.New(orderenums.ErrStatusTransition)
	}

	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.orders.LockByIDTx(ctx, tx, "", req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		if !canTransition(e.Status, to) {
			return errors.New(transitionError(e.Status, to))
		}
		fields := map[string]any{"status": to, "update_time": now}
		// 付款与完成各有自己的时间戳（对账与时效统计要用）。
		switch to {
		case ordermodel.OrderStatusPaid:
			fields["paid_at"] = now
		case ordermodel.OrderStatusCompleted:
			fields["completed_at"] = now
		case ordermodel.OrderStatusShipped:
			fields["completed_at"] = nil
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
			return uerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     to,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       strings.TrimSpace(req.Remark),
			CreateTime:   now,
		})
	})
}

// CancelOrder 取消订单：改状态 + 归还库存。
//
// 顺序照建单的同一套规则：**先落账、后动库存、失败留痕**。
// 归还失败时状态已经是取消（对用户而言结论正确），额外记一条流水说明
// 「库存归还未完成，需人工处理」—— 少还了库存是商家吃亏，留着痕比回滚状态好定位。
func (s *Service) CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (res *orderdto.CancelOrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, errors.New(orderenums.ErrCancelReasonRequired)
	}

	now := time.Now()
	var orderNo, projectID string
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		// 工程作用域（DB-009 第三批）：orders 带 FORCE 策略，加锁读、状态更新、状态日志
		// 三步都在这个事务里 —— 缺少 app.project_id 时它们会**静默**匹配 0 行 / 写不进去。
		// 调用方明确知道工程时（超时取消扫描逐工程调用）必须传下来。
		if pid := strings.TrimSpace(req.ProjectID); pid != "" {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
		}
		e, lerr := s.orders.LockByIDTx(ctx, tx, strings.TrimSpace(req.ProjectID), req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		if !canTransition(e.Status, ordermodel.OrderStatusCancelled) {
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusCancelled))
		}
		orderNo, projectID = e.OrderNo, e.ProjectID
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.OrderStatusCancelled,
			"cancel_reason": reason,
			"update_time":   now,
		}); uerr != nil {
			return uerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     ordermodel.OrderStatusCancelled,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       reason,
			CreateTime:   now,
		})
	})
	if err != nil {
		return nil, err
	}

	// 释放优惠码核销（与建单 redeem 对称；失败不阻断取消主链）。
	s.releaseCouponForOrder(ctx, req.OrderID)

	// 归还库存（跨模块，事务之外）。只归还**还没发货**的单：已发货的取消发生在
	// 货物已出库之后，归还应该走退货入库流程（有实物验收环节），不能凭空加回来。
	items, ierr := s.items.ListByOrderID(ctx, req.OrderID)
	if ierr != nil {
		return nil, ierr
	}
	if len(items) == 0 {
		return nil, errors.New(orderenums.ErrOrderHasNoItems)
	}
	lines := make([]ordercontract.StockLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, ordercontract.StockLine{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			SKUCode:   it.SKU,
			Quantity:  it.Quantity,
		})
	}
	if rerr := s.stock.ChangeStock(ctx, &ordercontract.StockAdjustment{
		ProjectID:  projectID,
		ReasonCode: "return_in",
		SourceType: "order",
		SourceRef:  orderNo,
		Remark:     "取消订单归还库存：" + reason,
		Lines:      lines,
	}); rerr != nil {
		// 留痕不阻断：状态已经是取消（对调用方而言结论正确），库存归属问题留给人工处理。
		// 用**独立事务**写 —— 上面那个事务已经提交，拿它的句柄再写会 panic（tx 为 nil）。
		warn := orderenums.MsgCancelledStockWarning + "：" + rerr.Error()
		if lerr := s.logs.Transaction(ctx, func(tx *gorm.DB) error {
			return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
				OrderID:      req.OrderID,
				FromStatus:   ordermodel.OrderStatusCancelled,
				ToStatus:     ordermodel.OrderStatusCancelled,
				OperatorType: ordermodel.OperatorTypeSystem,
				Remark:       "库存归还未完成，需人工处理：" + rerr.Error(),
				CreateTime:   time.Now(),
			})
		}); lerr != nil {
			logger.Scene("order").With("order_id", req.OrderID).Error(lerr, "库存归还失败且留痕写入失败")
		}
		return &orderdto.CancelOrderResp{Warnings: []string{warn}}, nil
	}
	return &orderdto.CancelOrderResp{}, nil
}

// RefundOrder 退款：改状态 + 记支付流水号。**不归还库存**（理由见契约注释）。
func (s *Service) RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error) {
	if req == nil || req.OrderID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.orders.LockByIDTx(ctx, tx, "", req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		if !canTransition(e.Status, ordermodel.OrderStatusRefunded) {
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusRefunded))
		}
		fields := map[string]any{"status": ordermodel.OrderStatusRefunded, "update_time": now}
		if tid := strings.TrimSpace(req.TransactionID); tid != "" {
			fields["transaction_id"] = tid
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
			return uerr
		}
		return s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     ordermodel.OrderStatusRefunded,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       strings.TrimSpace(req.Reason),
			CreateTime:   now,
		})
	})
}

// transitionError 依据当前状态给出更具体的话，而不是一律「不支持该操作」。
func transitionError(from, to string) string {
	switch from {
	case ordermodel.OrderStatusCancelled:
		return orderenums.ErrAlreadyCancelled
	case ordermodel.OrderStatusRefunded:
		return orderenums.ErrAlreadyRefunded
	case ordermodel.OrderStatusCompleted:
		if to == ordermodel.OrderStatusCancelled {
			return orderenums.ErrOrderNotCancellable
		}
	case ordermodel.OrderStatusShipped:
		if to == ordermodel.OrderStatusCancelled {
			return orderenums.ErrOrderNotCancellable
		}
	case ordermodel.OrderStatusPending, ordermodel.OrderStatusPaid:
		if to == ordermodel.OrderStatusRefunded {
			return orderenums.ErrOrderNotRefundable
		}
	}
	return orderenums.ErrStatusTransition
}
