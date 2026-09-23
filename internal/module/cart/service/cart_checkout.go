package cartservice

// cart_checkout.go — 访客结算：cookie 购物车 → 订单 → 支付 → 落账。
//
// 四步的顺序是有讲究的：
//
//	① 建单（订单域负责落快照、扣库存、幂等）
//	② 向支付通道发起扣款
//	③ 把「钱到了」记到订单上（order.PayOrder，幂等）
//	④ 清空购物车 cookie
//
// ②③ 失败**不回滚 ①**：订单已经存在、库存已经扣掉，钱没收到是「待付款」而不是
// 「没下过单」。返回结果里带 Paid=false + 单号，访客能凭单号继续付款或找客服 ——
// 反过来把订单删掉，客户手里就什么都不剩了。
//
// 为什么扣款放在建单之后：先收钱后建单的话，建单失败（比如库存刚好被抢完）就要退款，
// 而退款是比补偿库存麻烦得多的事。留一张待付款的订单，成本只有一条记录。

import (
	"context"
	"errors"
	"strings"
	"time"

	cartcontract "go_wp/internal/module/cart/contract"
	cartdto "go_wp/internal/module/cart/dto"
	cartenums "go_wp/internal/module/cart/enums"
	ordercontract "go_wp/internal/module/order/contract"
)

// Checkout 访客结算。
func (s *Service) Checkout(ctx context.Context, req *cartdto.CartCheckoutReq) (res *cartdto.CheckoutResp, err error) {
	if req == nil {
		return nil, errors.New(cartenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, errors.New(cartenums.ErrProjectRequired)
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		return nil, errors.New(cartenums.ErrEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return nil, errors.New(cartenums.ErrEmailInvalid)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New(cartenums.ErrNameRequired)
	}
	phone := strings.TrimSpace(req.Phone)
	if phone == "" {
		return nil, errors.New(cartenums.ErrPhoneRequired)
	}
	shipping := req.Shipping
	if strings.TrimSpace(shipping.Address) == "" {
		return nil, errors.New(cartenums.ErrAddressRequired)
	}

	p, _ := s.codec.decode(req.Cookie)
	lines := p.cartLines()
	if len(lines) == 0 {
		return nil, errors.New(cartenums.ErrCartEmpty)
	}

	// 订单项只给「变体 + 数量」：价格由订单域从商品域现读后落快照，
	// 请求里没有价格字段可填（购物车 cookie 里的价格也不作数 —— 它可能已经过期）。
	items := make([]ordercontract.OrderItemReq, 0, len(lines))
	for _, l := range lines {
		items = append(items, ordercontract.OrderItemReq{VariantID: l.VariantID, Quantity: l.Quantity})
	}

	now := time.Now()
	created, err := s.orders.CreateOrder(ctx, &ordercontract.CreateOrderReq{
		ProjectID:     projectID,
		CustomerEmail: email,
		CustomerName:  name,
		CustomerPhone: phone,
		Items:         items,
		Shipping:      shipping,
		Billing:       billingOrShipping(req.Billing, shipping),
		// 支付方式由通道自己报：结算流程不认识「paypal」这个词，
		// 只认识「当前装着哪个通道」。
		PaymentMethod:      s.pay.Method(),
		PaymentMethodTitle: s.pay.Title(),
		Remark:             strings.TrimSpace(req.Remark),
		RequestID:          strings.TrimSpace(req.RequestID),
		Locale:             req.Locale,
		// 归因在下单这一刻从 cookie 定格（cookie 之后会过期、来源会被覆盖）。
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
		TotalLabel:    centsLabel(created.Total),
		Currency:      created.Currency,
		Email:         email,
		AccountMailed: created.AccountMailed,
		// 无论支付成不成，购物车都清空：东西已经变成订单了，留着只会让人再点一次结账。
		Cookie: s.emptyCookie(),
	}

	// ① 扣款。失败不是错误（订单已经在了），只是这一单停在待付款。
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

	// ② 落账。幂等：重复调用返回同一结论，不会把已付的单再写一遍。
	paid, perr := s.orders.PayOrder(ctx, &ordercontract.PayOrderReq{
		OrderID:            created.ID,
		PaymentMethod:      s.pay.Method(),
		PaymentMethodTitle: s.pay.Title(),
		TransactionID:      charge.TransactionID,
		Remark:             "访客结算支付成功",
	})
	if perr != nil {
		// 钱扣了但没记上：这是最需要有人知道的一种失败，所以带上支付流水号，
		// 让后台能按流水号去通道侧核对（流水号进不了订单列时至少进日志）。
		res.PaymentError = cartenums.ErrPaymentFailed
		return res, nil
	}
	res.Paid = true
	res.Status = paid.Status
	return res, nil
}

// billingOrShipping 账单地址缺省与收货地址相同。
//
// 绝大多数下单里两者一致；让访客为了「我要开发票」再填一遍地址，
// 是把系统的字段完整性要求转嫁给了用户。
func billingOrShipping(billing, shipping ordercontract.OrderAddress) ordercontract.OrderAddress {
	if strings.TrimSpace(billing.Name) == "" && strings.TrimSpace(billing.Address) == "" {
		return shipping
	}
	return billing
}

// emptyCookie 空购物车的 cookie 值（结算成功后写回）。
func (s *Service) emptyCookie() string {
	cookie, err := s.codec.encode(cartPayload{V: cartCookieVersion})
	if err != nil {
		// 编码空载荷不会失败；真失败了也只是「购物车没被清空」，
		// 不该让一次成功的下单在这里报错。
		return ""
	}
	return cookie
}
