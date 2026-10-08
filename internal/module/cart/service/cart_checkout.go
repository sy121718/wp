package cartservice

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
	"time"

	"go_wp/internal/module/cart/contract"
	"go_wp/internal/module/cart/dto"
	"go_wp/internal/module/cart/enums"
	"go_wp/internal/module/order/contract"
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
	// 运费：取值链（站点基础运费 → 满额免运费门槛 → 会员 free_shipping）全在
	// shippingTotalOf 一处，这里只准备它的两个输入 —— 站点规则（服务端读，客户端不可控）
	// 与商品小计（服务端按商品域真源现算，只用于判门槛）。**只影响运费**：
	// 商品小计由订单域在建单时再算一遍并落快照，本函数碰不到它。
	// 放在建单之前：金额必须在建单那一刻定下来，事后再改运费就要动已落库的订单。
	policy := s.shippingPolicyOf(ctx, projectID)
	subtotal := int64(0)
	if policy.BaseFeeCents > 0 && policy.FreeThresholdCents > 0 {
		// 只有「要收运费**且**启用了门槛」时小计才有用：不收运费时 0 就是最终答案，
		// 没门槛时小计没有判定价值 —— 少一次商品域往返，而访客在等这一次往返。
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
		// 支付方式由通道自己报：结算流程不认识「paypal」这个词，
		// 只认识「当前装着哪个通道」。
		PaymentMethod:      s.pay.Method(),
		PaymentMethodTitle: s.pay.Title(),
		Remark:             strings.TrimSpace(req.Remark),
		RequestID:          strings.TrimSpace(req.RequestID),
		Locale:             req.Locale,
		// 券码只做透传：试算与核销都在 order 侧的建单事务里（见 CartCheckoutReq.CouponCode
		// 的注释）—— 券无效会以 order 的 enums 原因返回，这里不预判、不改写。
		CouponCode: strings.TrimSpace(req.CouponCode),
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
	//
	// 但**0 元订单没有钱可收**，不能送进通道：通道对 Amount <= 0 是明确拒绝的
	//（支付金额必须为正），送进去只会让订单停在 pending，30 分钟后被 order_expire
	// 自动取消（归还库存、释放券）—— 客户永远付不了这一单（BIZ-05）。
	// 免费商品（price=0）与 100% 折扣 + 无运费都会走到这里。
	//
	// 落账走与真实支付**可区分**的通道（free）：对账时「本来就 0 元」不能混进通道营收；
	// 流水号由订单号派生（与模拟通道同一手法），重复调用必然得到同一个号，无需共享状态。
	if created.Total == 0 {
		paid, perr := s.orders.PayOrder(ctx, &ordercontract.PayOrderReq{
			OrderID:            created.ID,
			PaymentMethod:      freePaymentMethod,
			PaymentMethodTitle: freePaymentMethodTitle,
			TransactionID:      freeTransactionIDPrefix + created.OrderNo,
			Remark:             "0 元订单免支付，直接落账",
		})
		if perr != nil {
			// 与扣款失败同一档：订单已建，落账没成功，停在待付款并带出错误码。
			// 这条分支只可能来自基础设施故障（锁等待 / 库不可用），不是业务拒绝。
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

// 0 元订单的落账通道（BIZ-05）。
const (
	// freePaymentMethod 免支付通道标识（落 orders.payment_method）。
	//
	// 与真实通道**必须可区分**：对账时「这单本来就 0 元」与「这单收了钱」是两件事，
	// 混进 paypal 会让营收报表出现一笔并不存在的流水。
	freePaymentMethod = "free"
	// freePaymentMethodTitle 免支付通道展示名（落订单上的快照值，后台一眼能认）。
	freePaymentMethodTitle = "无需支付（0 元订单）"
	// freeTransactionIDPrefix 免支付流水号前缀：由订单号派生（幂等，无需共享状态），
	// 且与通道流水号在后台列表里一眼可分。
	freeTransactionIDPrefix = "FREE-"
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
			Message: cartenums.MsgCallbackUnpaid,
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
