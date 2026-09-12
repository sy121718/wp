package orderservice

// order_pay.go — 支付落账（BIZ-1 销售侧）。
//
// 「谁去扣钱」不在订单域：网关（PayPal / Stripe / 微信支付）是外部系统，订单域只负责
// 「钱到了之后把这一单记成已付款」。这条边界让支付通道可以换、可以加、可以在测试里换成
// 假实现，而订单表与状态机一个字都不用改。
//
// 幂等是这一层最重要的性质：网关的异步通知会重发、访客会连点两次下单按钮、
// 上层会失败重试。重复到达时**必须返回和第一次相同的结果**，而不是报「状态不支持」——
// 报错会让网关一直重试，也让用户的第二次点击变成一次报错弹窗。

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
)

// PayOrder 支付落账：pending → paid。
//
// 行锁内判定，所以并发的两次「支付成功」（比如用户双击 + 浏览器重发）只会有一条改动列，
// 另一条走幂等分支原样返回。
func (s *Service) PayOrder(ctx context.Context, req *orderdto.PayOrderReq) (res *orderdto.PayOrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	method := strings.TrimSpace(req.PaymentMethod)
	if method == "" {
		// 不落支付通道就置为已付款，等于在账上放一笔来路不明的钱。
		return nil, errors.New(orderenums.ErrPaymentMethodRequired)
	}
	title := strings.TrimSpace(req.PaymentMethodTitle)
	txnID := strings.TrimSpace(req.TransactionID)
	remark := strings.TrimSpace(req.Remark)
	now := time.Now()

	res = &orderdto.PayOrderResp{}
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		e, lerr := s.orders.LockByIDTx(ctx, tx, req.OrderID)
		if lerr != nil {
			return lerr
		}
		if e == nil {
			return errors.New(orderenums.ErrOrderNotFound)
		}

		// 幂等分支：已经付过（或已经走到更靠后的状态）就原样返回，不改任何列。
		// paid / shipped / completed 三个状态都算「钱已经收到了」——
		// 一笔已发货的订单收到重复的支付通知，结论仍然是「已支付」。
		switch e.Status {
		case ordermodel.OrderStatusPaid, ordermodel.OrderStatusShipped, ordermodel.OrderStatusCompleted:
			res.ID, res.OrderNo, res.Status = e.ID, e.OrderNo, e.Status
			res.TransactionID = e.TransactionID
			res.AlreadyPaid = true
			return nil
		case ordermodel.OrderStatusCancelled, ordermodel.OrderStatusRefunded:
			// 已取消（比如库存不足的补偿取消）的单收到支付成功，是真的异常：
			// 钱可能真的扣了，但不能靠改状态掩盖过去，留给人工按单号退款。
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusPaid))
		}
		if !canTransition(e.Status, ordermodel.OrderStatusPaid) {
			return errors.New(transitionError(e.Status, ordermodel.OrderStatusPaid))
		}

		fields := map[string]any{
			"status":         ordermodel.OrderStatusPaid,
			"paid_at":        now,
			"update_time":    now,
			"payment_method": method,
		}
		// 标题与流水号是可选信息：模拟通道也会给，但真通道的某些回调可能不带。
		if title != "" {
			fields["payment_method_title"] = title
		}
		if txnID != "" {
			fields["transaction_id"] = txnID
		}
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ID, fields); uerr != nil {
			return uerr
		}
		if cerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
			OrderID:      e.ID,
			FromStatus:   e.Status,
			ToStatus:     ordermodel.OrderStatusPaid,
			OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeSystem),
			OperatorID:   req.OperatorID,
			OperatorName: strings.TrimSpace(req.OperatorName),
			Remark:       defaultString(remark, "支付成功"),
			CreateTime:   now,
		}); cerr != nil {
			return cerr
		}
		res.ID, res.OrderNo, res.Status = e.ID, e.OrderNo, ordermodel.OrderStatusPaid
		res.TransactionID = txnID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
