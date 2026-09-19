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

	// 定位跳（DB-009 第四批）：请求只给订单 id，而 orders 带 FORCE 策略 —— 缺作用域时
	// 下面这条加锁读在非超级角色下拿不到行，接口会报「订单不存在」。
	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return perr
	}
	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.orders.LockByIDTx(ctx, tx, projectID, req.OrderID)
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

// CancelOrder 取消订单：改状态 + 记流转 + **同一事务内**归还库存 + 释放券核销。
//
// 全有或全无：任一步失败（含库存归还）整体回滚，订单仍是取消前的状态。
// 原先的「先提交状态、再动库存、失败写一条 Warnings 留痕」是典型的跨模块补偿 ——
// 库存没回来而订单已取消，只能靠人工对账；同库跨模块按 AGENTS.md 一律**事务透传**
// （库存句柄经 ChangeStockTx 传进本事务），失败就没有半截状态可言。
func (s *Service) CancelOrder(ctx context.Context, req *orderdto.CancelOrderReq) (res *orderdto.CancelOrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return nil, errors.New(orderenums.ErrCancelReasonRequired)
	}

	// 工程作用域（DB-009 第三批 + 第四批）：orders 带 FORCE 策略，加锁读、状态更新、
	// 状态日志三步都在这个事务里 —— 缺 app.project_id 时它们会**静默**匹配 0 行 / 写不进去。
	// 调用方显式给了工程（超时取消扫描逐工程调用）就用它；没给（后台取消按钮）则
	// 事务外逐工程探测出归属 —— 不留「不限工程」这条路径。
	scopeProjectID, perr := s.resolveOrderProject(ctx, req.OrderID, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	now := time.Now()
	var orderNo string
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, scopeProjectID); serr != nil {
			return serr
		}
		e, lerr := s.orders.LockByIDTx(ctx, tx, scopeProjectID, req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}
		if !canTransition(e.Status, ordermodel.OrderStatusCancelled) {
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusCancelled))
		}
		orderNo = e.OrderNo
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, map[string]any{
			"status":        ordermodel.OrderStatusCancelled,
			"cancel_reason": reason,
			"update_time":   now,
		}); uerr != nil {
			return uerr
		}
		if lerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     ordermodel.OrderStatusCancelled,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeAdmin),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       reason,
			CreateTime:   now,
		}); lerr != nil {
			return lerr
		}

		// 归还库存（同库跨模块，句柄透传进本事务）。只归还**还没发货**的单：已发货的取消
		// 发生在货物已出库之后，归还应该走退货入库流程（有实物验收环节），不能凭空加回来。
		items, ierr := s.items.ListByOrderIDTx(ctx, tx, e.ID)
		if ierr != nil {
			return ierr
		}
		if len(items) == 0 {
			return errors.New(orderenums.ErrOrderHasNoItems)
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
		if rerr := s.stock.ChangeStockTx(ctx, tx, &ordercontract.StockAdjustment{
			ProjectID:  e.ProjectID,
			ReasonCode: "return_in",
			SourceType: "order",
			SourceRef:  orderNo,
			Remark:     "取消订单归还库存：" + reason,
			Lines:      lines,
		}); rerr != nil {
			// 归还失败 → 整个取消回滚：订单仍是原状态，库存也没动，不存在需要人工兜底的半截状态。
			return rerr
		}

		// 释放优惠码核销（与建单 redeem 对称）：同一事务 —— 券的 used_count 与核销明细
		// 要么都回退、要么都不动，不再出现「订单取消了、券次数没还」的偏差。
		if s.coupons != nil {
			if _, crerr := s.coupons.ReleaseRedemptionByOrderTx(ctx, tx, e.ID); crerr != nil {
				return crerr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &orderdto.CancelOrderResp{}, nil
}

// RefundOrder 退款：改状态 + 记支付流水号。**不归还库存**（理由见契约注释）。
func (s *Service) RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error) {
	if req == nil || req.OrderID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	// 定位跳（DB-009 第四批）：同 ChangeStatus —— 请求只给订单 id。
	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return perr
	}
	now := time.Now()
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		e, lerr := s.orders.LockByIDTx(ctx, tx, projectID, req.OrderID)
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
