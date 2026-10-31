package cartservice

import (
	"context"
	"strings"
	"time"

	"go_wp/internal/module/cart/contract"
	"go_wp/internal/module/cart/dto"
	"go_wp/internal/module/cart/enums"
	"go_wp/internal/module/order/contract"
)

func (s *Service) Checkout(ctx context.Context, req *cartdto.CartCheckoutReq) (res *cartdto.CheckoutResp, err error) {
	if req == nil {
		return nil, cartenums.Err(cartenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, cartenums.Err(cartenums.ErrProjectRequired)
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return nil, cartenums.Err(cartenums.ErrEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return nil, cartenums.Err(cartenums.ErrEmailInvalid)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, cartenums.Err(cartenums.ErrNameRequired)
	}
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		return nil, cartenums.Err(cartenums.ErrPhoneRequired)
	}
	shipping := req.Shipping
	if strings.TrimSpace(shipping.Name) == "" {
		shipping.Name = name
	}
	if strings.TrimSpace(shipping.Phone) == "" {
		shipping.Phone = phone
	}
	if strings.TrimSpace(shipping.Address) == "" {
		return nil, cartenums.Err(cartenums.ErrAddressRequired)
	}

	p, _ := s.codec.decode(req.Cookie)
	lines := p.cartLines()
	if len(lines) == 0 {
		return nil, cartenums.Err(cartenums.ErrCartEmpty)
	}

	items := make([]ordercontract.OrderItemReq, 0, len(lines))
	for _, l := range lines {
		items = append(items, ordercontract.OrderItemReq{VariantID: l.VariantID, Quantity: l.Quantity})
	}

	now := time.Now()

	policy := s.shippingPolicyOf(ctx, projectID)
	subtotal := int64(0)
	if policy.BaseFeeCents > 0 && policy.FreeThresholdCents > 0 {

		subtotal = s.cartSubtotalOf(ctx, projectID, lines)
	}
	shippingTotal := s.shippingTotalOf(ctx, projectID, req.UserID, subtotal, policy)
	created, err := s.orders.CreateOrder(ctx, &ordercontract.CreateOrderReq{
		ProjectID:     projectID,
		CustomerEmail: email,
		CustomerName:  name,
		CustomerPhone: phone,
		Items:         items,
		Shipping:      shipping,
		Billing:       billingOrShipping(req.Billing, shipping),
		ShippingTotal: shippingTotal,

		PaymentMethod:      s.pay.Method(),
		PaymentMethodTitle: s.pay.Title(),
		Remark:             strings.TrimSpace(req.Remark),
		RequestID:          strings.TrimSpace(req.RequestID),
		Locale:             req.Locale,

		CouponCode: strings.TrimSpace(req.CouponCode),

		Attribution: buildAttribution(req.Tracking, req.UserAgent, now),
		UserID:      req.UserID,
		IPAddress:   req.IPAddress,
		UserAgent:   req.UserAgent,
	})
	if err != nil {
		return nil, err
	}

	res = &cartdto.CheckoutResp{
		OrderID:       created.ID,
		OrderNo:       created.OrderNo,
		Status:        created.Status,
		Total:         created.Total,
		Currency:      created.Currency,
		Email:         email,
		AccountMailed: created.AccountMailed,

		Cookie: s.emptyCookie(),
	}

	if created.Total == 0 {
		paid, perr := s.orders.PayOrder(ctx, &ordercontract.PayOrderReq{
			OrderID:            created.ID,
			PaymentMethod:      freePaymentMethod,
			PaymentMethodTitle: freePaymentMethodTitle,
			TransactionID:      freeTransactionIDPrefix + created.OrderNo,
			Remark:             "0 元订单免支付，直接落账",
		})
		if perr != nil {

			res.PaymentError = cartenums.ErrPaymentFailed
			return res, nil
		}
		res.Paid = true
		res.Status = paid.Status
		return res, nil
	}

	charge, cerr := s.pay.Charge(ctx, &cartcontract.PaymentChargeReq{
		OrderNo:  created.OrderNo,
		Amount:   created.Total,
		Currency: created.Currency,
		Email:    email,
	})
	if cerr != nil {
		res.PaymentError = cartenums.ErrPaymentFailed
		return res, nil
	}

	paid, perr := s.orders.PayOrder(ctx, &ordercontract.PayOrderReq{
		OrderID:            created.ID,
		PaymentMethod:      s.pay.Method(),
		PaymentMethodTitle: s.pay.Title(),
		TransactionID:      charge.TransactionID,
		Remark:             "访客结算支付成功",
	})
	if perr != nil {

		res.PaymentError = cartenums.ErrPaymentFailed
		return res, nil
	}
	res.Paid = true
	res.Status = paid.Status
	return res, nil
}

func billingOrShipping(billing, shipping ordercontract.OrderAddress) ordercontract.OrderAddress {
	if strings.TrimSpace(billing.Name) == "" && strings.TrimSpace(billing.Address) == "" {
		return shipping
	}
	return billing
}

func (s *Service) emptyCookie() string {
	cookie, err := s.codec.encode(cartPayload{V: cartCookieVersion})
	if err != nil {

		return ""
	}
	return cookie
}

const (
	freePaymentMethod = "free"

	freePaymentMethodTitle = "无需支付（0 元订单）"

	freeTransactionIDPrefix = "FREE-"
)

func (s *Service) HandlePaymentCallback(ctx context.Context, req *cartdto.PaymentCallbackReq) (res *cartdto.PaymentCallbackResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, cartenums.Err(cartenums.ErrProjectRequired)
	}

	cb, cerr := s.pay.VerifyCallback(req.Headers, req.RawBody)
	if cerr != nil {
		return nil, cartenums.Err(cartenums.ErrCallbackSignature)
	}

	order, oerr := s.orders.GetOrderByNo(ctx, &ordercontract.GetOrderByNoReq{
		ProjectID: req.ProjectID,
		OrderNo:   cb.OrderNo,
	})
	if oerr != nil {
		if strings.Contains(oerr.Error(), ordercontract.ErrOrderNotFound) {
			return nil, cartenums.Err(cartenums.ErrCallbackOrderMissing)
		}
		return nil, oerr
	}

	if cb.Amount > 0 && cb.Amount != order.Total {
		return nil, cartenums.Err(cartenums.ErrCallbackAmountMismatch)
	}

	if !cb.Paid {
		return &cartdto.PaymentCallbackResp{
			OrderID: order.ID,
			OrderNo: order.OrderNo,
			Status:  order.Status,
			Paid:    false,
			Message: cartenums.MsgCallbackUnpaid,
		}, nil
	}

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
	message := cartenums.MsgCallbackApplied
	if paid.NeedsManualReview {
		message = cartenums.MsgCallbackNeedsReview
	} else if paid.AlreadyPaid {
		message = cartenums.MsgCallbackAlreadyPaid
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
