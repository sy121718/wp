package orderservice

// order_create_draft.go — 建单的「备料」阶段：事务之前能算完的一切。
//
// 这一段的共同点是**只读**：商品事实来自 product 契约，优惠码试算是纯读，
// 归因只是把请求里的快照转成 JSONB，访客开号失败也不阻断下单。
// 把它们与事务写入分开，是为了让「钱怎么算出来的」这件事可以单独读、单独测 ——
// 金额与折扣此前挤在 CreateOrder 的中段，正是审计里两个资损缺陷的藏身处。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	userdto "go_wp/internal/module/user/dto"
)

// orderDraft 建单的中间结果：订单头、订单项、命中的优惠码、访客开号结果。
//
// head.ID 在事务写入之后才被填上（由 persistOrder 回填），因为订单项要挂它。
type orderDraft struct {
	projectID     string
	head          *ordermodel.OrderEntity
	items         []*ordermodel.OrderItemEntity
	appliedCoupon *ordermodel.CouponEntity
	accountMailed bool
	now           time.Time
}

// response 建单成功（或未重复）时的响应。
func (d *orderDraft) response() *orderdto.CreateOrderResp {
	return &orderdto.CreateOrderResp{
		ID: d.head.ID, OrderNo: d.head.OrderNo, Status: d.head.Status,
		Total: d.head.Total, Currency: d.head.Currency, Duplicated: false,
		AccountMailed: d.accountMailed,
	}
}

// buildOrderDraft 把请求变成一个**可以直接落库**的草稿。
//
// 步骤顺序与拆分前逐字一致（快照 → 金额 → 券 → 开号 → 订单号 → 订单头）：
// 券要等小计算出来才能试算，访客开号要在订单号之前（开号结果进订单头）。
func (s *Service) buildOrderDraft(ctx context.Context, req *orderdto.CreateOrderReq, projectID string) (*orderDraft, error) {
	now := time.Now()
	email := strings.TrimSpace(req.CustomerEmail)

	items, subtotal, err := s.buildOrderItems(ctx, req, projectID, now)
	if err != nil {
		return nil, err
	}

	appliedCoupon, discount, err := s.resolveCoupon(ctx, projectID, req.CouponCode, subtotal)
	if err != nil {
		return nil, err
	}
	// 金额：小计 - 优惠 + 运费 + 税。优惠不得低于 0、也不得超过小计（负数总额没有意义）。
	if discount < 0 {
		discount = 0
	}
	if discount > subtotal {
		discount = subtotal
	}
	allocateLineDiscounts(items, subtotal, discount)
	shipping := req.ShippingTotal
	if shipping < 0 {
		shipping = 0
	}
	total := subtotal - discount + shipping

	// 访客开号：邮箱还没有账号就建一个（随机初始密码，邮件发给客户）。
	userID, accountMailed := s.ensureGuestAccount(ctx, req)

	// 每人限次在 redeemCouponTx 内与核销同事务判定（行锁 + 计数），
	// 避免事务外先读再写被并发绕过。

	orderNo, err := s.newOrderNo(ctx, projectID)
	if err != nil {
		return nil, err
	}

	head := &ordermodel.OrderEntity{
		ProjectID:          projectID,
		OrderNo:            orderNo,
		Status:             ordermodel.OrderStatusPending,
		UserID:             userID,
		CustomerEmail:      email,
		CustomerName:       strings.TrimSpace(req.CustomerName),
		CustomerPhone:      strings.TrimSpace(req.CustomerPhone),
		Currency:           "CNY",
		Subtotal:           subtotal,
		DiscountTotal:      discount,
		ShippingTotal:      shipping,
		TaxTotal:           0,
		Total:              total,
		ShipName:           strings.TrimSpace(req.Shipping.Name),
		ShipPhone:          strings.TrimSpace(req.Shipping.Phone),
		ShipProvince:       strings.TrimSpace(req.Shipping.Province),
		ShipCity:           strings.TrimSpace(req.Shipping.City),
		ShipDistrict:       strings.TrimSpace(req.Shipping.District),
		ShipAddress:        strings.TrimSpace(req.Shipping.Address),
		ShipZip:            strings.TrimSpace(req.Shipping.Zip),
		BillName:           strings.TrimSpace(req.Billing.Name),
		BillPhone:          strings.TrimSpace(req.Billing.Phone),
		BillProvince:       strings.TrimSpace(req.Billing.Province),
		BillCity:           strings.TrimSpace(req.Billing.City),
		BillDistrict:       strings.TrimSpace(req.Billing.District),
		BillAddress:        strings.TrimSpace(req.Billing.Address),
		BillZip:            strings.TrimSpace(req.Billing.Zip),
		PaymentMethod:      strings.TrimSpace(req.PaymentMethod),
		PaymentMethodTitle: strings.TrimSpace(req.PaymentMethodTitle),
		CreatedVia:         defaultString(req.CreatedVia, ordermodel.CreatedViaCheckout),
		IPAddress:          strings.TrimSpace(req.IPAddress),
		UserAgent:          strings.TrimSpace(req.UserAgent),
		RequestID:          strings.TrimSpace(req.RequestID),
		Remark:             strings.TrimSpace(req.Remark),
		AdminNote:          strings.TrimSpace(req.AdminNote),
		CreateBy:           req.CreateBy,
		CreateTime:         now,
		UpdateTime:         now,
	}
	if head.Attribution, err = marshalAttribution(req.Attribution); err != nil {
		return nil, err
	}

	return &orderDraft{
		projectID:     projectID,
		head:          head,
		items:         items,
		appliedCoupon: appliedCoupon,
		accountMailed: accountMailed,
		now:           now,
	}, nil
}

// buildOrderItems 落订单项快照并算出小计。
//
// **价格全部来自服务端**：请求体里没有价格字段，客户端能传价格的接口等于把收银台
// 交给客人自己看。商品事实一次批量取（不逐条查），每项都要过归属、启用与数量校验。
func (s *Service) buildOrderItems(ctx context.Context, req *orderdto.CreateOrderReq, projectID string, now time.Time) ([]*ordermodel.OrderItemEntity, int64, error) {
	variantIDs := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		variantIDs = append(variantIDs, strings.TrimSpace(it.VariantID))
	}
	snapshots, err := s.product.VariantSnapshots(ctx, variantIDs)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]*productcontract.VariantSnapshot, len(snapshots))
	for _, sn := range snapshots {
		byID[sn.VariantID] = sn
	}

	items := make([]*ordermodel.OrderItemEntity, 0, len(req.Items))
	var subtotal int64
	for _, it := range req.Items {
		vid := strings.TrimSpace(it.VariantID)
		if vid == "" {
			return nil, 0, errors.New(orderenums.ErrInvalidParam)
		}
		if it.Quantity <= 0 || it.Quantity > maxItemQuantity {
			return nil, 0, errors.New(orderenums.ErrQuantityInvalid)
		}
		sn := byID[vid]
		if sn == nil {
			return nil, 0, fmt.Errorf("%w: %s", errors.New(orderenums.ErrVariantNotFound), vid)
		}
		if sn.ProjectID != "" && sn.ProjectID != projectID {
			// 跨工程下单是越权，不是「查不到」。
			return nil, 0, errors.New(orderenums.ErrVariantNotFound)
		}
		if !sn.Enabled {
			return nil, 0, errors.New(orderenums.ErrVariantNotFound)
		}
		lineSubtotal := sn.Price * int64(it.Quantity)
		subtotal += lineSubtotal
		items = append(items, &ordermodel.OrderItemEntity{
			ProductID:    sn.ProductID,
			VariantID:    sn.VariantID,
			ProductName:  sn.ProductName,
			VariantLabel: sn.VariantLabel,
			SKU:          sn.SKU,
			UnitPrice:    sn.Price,
			Quantity:     it.Quantity,
			LineSubtotal: lineSubtotal,
			LineDiscount: 0,
			LineTax:      0,
			LineTotal:    lineSubtotal,
			CostPrice:    sn.CostPrice,
			CreateTime:   now,
		})
	}
	return items, subtotal, nil
}

// resolveCoupon 优惠码试算：给了码就以**服务端试算**的折扣为准。
//
// 这里只做「券自身」的判定（状态 / 时间窗 / 门槛 / 总数），每人限次要等 userID 解析
// 出来之后在核销事务里查（见下方注释），因为访客下单时账号是「这一单才建的」。
//
// **无优惠码时折扣恒为 0**：不采信调用方传入的 discountTotal（SEC-001）——
// 后台代客下单也不能自带折扣，人工折扣若要放开，必须走独立的字段 + 权限点 + 审计留痕，
// 而不是复用这个字段。
func (s *Service) resolveCoupon(ctx context.Context, projectID, couponCode string, subtotal int64) (*ordermodel.CouponEntity, int64, error) {
	code := normalizeCouponCode(couponCode)
	if code == "" {
		return nil, 0, nil
	}
	ce, err := s.coupons.GetByCode(ctx, projectID, code)
	if err != nil {
		return nil, 0, err
	}
	if ce == nil {
		return nil, 0, errors.New(orderenums.ErrCouponNotFound)
	}
	if reason := couponRuleCheck(ce, subtotal, time.Now()); reason != "" {
		return nil, 0, errors.New(reason)
	}
	return ce, couponDiscount(ce, subtotal), nil
}

// ensureGuestAccount 访客开号：邮箱还没有账号就建一个并把新账号关联到订单。
//
// 失败**不阻断下单**：订单是主体、账号是附赠能力；开号失败时订单照常落库、user_id 留空
// （客户仍可用这个邮箱走「忘记密码」自己开号）。
// 邮箱已有账号时只关联、**绝不改密码** —— 那条安全边界在 user 模块里守着。
//
// 返回值二：只有「这次确实新建了账号、且初始密码寄出去了」才为 true。
// 邮箱已有账号时我们只关联、绝不改密码，此时告诉客户「密码已发到你邮箱」
// 会让他在邮箱里白找一场。
func (s *Service) ensureGuestAccount(ctx context.Context, req *orderdto.CreateOrderReq) (*uint64, bool) {
	if req.UserID != nil || s.guest == nil {
		return req.UserID, false
	}
	gres, gerr := s.guest.EnsureGuestAccount(ctx, &userdto.GuestAccountReq{
		Email:      strings.TrimSpace(req.CustomerEmail),
		Name:       strings.TrimSpace(req.CustomerName),
		Locale:     req.Locale,
		RegisterIP: req.IPAddress,
	})
	if gerr != nil || gres == nil || gres.UserID == 0 {
		return req.UserID, false
	}
	id := gres.UserID
	return &id, gres.Created && gres.PasswordMailed
}
