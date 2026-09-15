package cartservice

// cart_callback.go — 支付通道的异步回调（BIZ-1）。
//
// 与 Checkout 的分工：Checkout 是「同步扣款 + 立刻落账」，本文件是「通道事后通知」。
// 两条路径最终都汇聚到同一个**幂等的** order.PayOrder 上，所以哪条先到、到几次，
// 结果都一样 —— 这正是把落账收成一个幂等操作的价值。
//
// 四步都不可以省：
//   ① 验签 —— 回调是外部打进来的，签名是唯一可依靠的来源证明；
//   ② 按商户单号找单 —— 通道只有单号，没有我们的自增 id；
//   ③ 金额核对 —— 与订单总额不符时宁可停在人工核对，也不入账；
//   ④ 幂等落账 —— 通道重发通知是常态，重复必须是无害的。

import (
	"context"
	"errors"
	"strings"

	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	ordercontract "go_wp/internal/module/order/contract"
)

// HandlePaymentCallback 处理支付通道的异步回调。
func (s *Service) HandlePaymentCallback(ctx context.Context, req *cartdto.PaymentCallbackReq) (res *cartdto.PaymentCallbackResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(cartenums.ErrProjectRequired)
	}

	// ① 验签。失败一律收成同一句话对外：不区分「没有签名 / 签名错 / 密钥不对」——
	// 那是给攻击者的信息。
	cb, cerr := s.pay.VerifyCallback(req.Headers, req.RawBody)
	if cerr != nil {
		return nil, errors.New(cartenums.ErrCallbackSignature)
	}

	// ② 按商户单号找单（通道不认识我们的自增 id）。
	order, oerr := s.orders.GetOrderByNo(ctx, &ordercontract.GetOrderByNoReq{
		ProjectID: req.ProjectID,
		OrderNo:   cb.OrderNo,
	})
	if oerr != nil {
		if strings.Contains(oerr.Error(), ordercontract.ErrOrderNotFound) {
			return nil, errors.New(cartenums.ErrCallbackOrderMissing)
		}
		return nil, oerr
	}

	// ③ 金额核对。cb.Amount 为 0 表示通道没报金额（不是「金额为零」），
	// 那种情况跳过核对而不是按 0 比对 —— 否则每一笔都会被判成不一致。
	if cb.Amount > 0 && cb.Amount != order.Total {
		return nil, errors.New(cartenums.ErrCallbackAmountMismatch)
	}

	// 支付失败通知：订单本就停在待付款，不改状态，但要给出明确结论。
	if !cb.Paid {
		return &cartdto.PaymentCallbackResp{
			OrderID: order.ID,
			OrderNo: order.OrderNo,
			Status:  order.Status,
			Paid:    false,
			Message: "通道通知支付未成功，订单保持待付款",
		}, nil
	}

	// ④ 幂等落账：已付款的单再做一次不会改任何列，只会如实回报「此前已付」。
	paid, perr := s.orders.PayOrder(ctx, &ordercontract.PayOrderReq{
		OrderID:            order.ID,
		PaymentMethod:      cb.Method,
		PaymentMethodTitle: cb.MethodTitle,
		TransactionID:      cb.TransactionID,
		Remark:             "支付通道异步回调",
	})
	if perr != nil {
		return nil, perr
	}
	message := "回调已入账"
	if paid.NeedsManualReview {
		message = "支付成功但订单已终态，已记流水待人工核对"
	} else if paid.AlreadyPaid {
		message = "订单此前已付款，本次未改动（幂等命中）"
	}
	return &cartdto.PaymentCallbackResp{
		OrderID: order.ID,
		OrderNo: order.OrderNo,
		Status:  paid.Status,
		Paid:    true,
		Already: paid.AlreadyPaid,
		Applied: !paid.AlreadyPaid,
		Message: message,
	}, nil
}
