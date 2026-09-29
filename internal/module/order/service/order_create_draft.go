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
	usercontract "go_wp/internal/module/user/contract"
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
// 步骤顺序：快照 → 券试算 → 开号 → 会员折扣 → 分摊 → 订单号 → 订单头。
// 三处顺序有硬理由，改动前先读一遍：
//
//	· 券要等小计算出来才能试算；
//	· 开号排在会员折扣之前（BIZ-3 新增）：会员身份按账号解析，而新访客的身份
//	  正是这一单才建出来的账号；
//	· 会员折扣排在分摊之前：退款按分摊后的行实付算，折扣不进分摊就会退多。
//
// 开号结果同时进订单头（user_id），所以它必然在订单头构造之前 —— 与拆分前一致。
func (s *Service) buildOrderDraft(ctx context.Context, req *orderdto.CreateOrderReq, projectID, createdVia string) (*orderDraft, error) {
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
	// 金额：小计 - 券 - 会员折扣 + 运费 + 税。两项折扣各自不得低于 0，
	// 合计也不得超过小计（负数总额没有意义，它会顺着 total 一路传到支付金额上）。
	if discount < 0 {
		discount = 0
	}
	if discount > subtotal {
		discount = subtotal
	}
	shipping := req.ShippingTotal
	if shipping < 0 {
		shipping = 0
	}

	// 访客开号：走**显式开关**（req.ProvisionGuestAccount）—— 后台代客建单页默认 false
	//（不开号、不发初始密码邮件），前台 checkout 不传该字段、保持既有行为。
	// 判定细节与三态语义见 ensureGuestAccount 与 dto.CreateOrderReq 的注释。
	//
	// 位置在会员折扣之前（原先在分摊之后）：会员身份按账号解析，而访客的身份恰恰是
	// 「这一单才建出来的账号」—— 折扣算在开号之前，新客户的第一单就永远拿不到会员价。
	userID, accountMailed := s.ensureGuestAccount(ctx, req)

	// 会员折扣（BIZ-3）：落在**券之后、分摊之前**。
	//   · 在券之后：相加扣减下两道折扣合计可能超过小计，会员折扣吃的是券扣完还剩的部分
	//     （上界夹在小计内，见 membershipDiscountAmount）；
	//   · 在分摊之前：退款按分摊后的行实付算，会员折扣若不参与分摊，
	//     部分退货就会按「没打过会员折扣」的行金额退 —— 那是资损，不是舍入差。
	// 独立计账：金额进 membership_discount_total，**不动** discount_total 的语义
	//（SEC-001「无优惠码时折扣恒为 0」那条判据必须继续成立）。
	membershipDiscount := s.resolveMembershipDiscount(ctx, projectID, userID, subtotal, discount)
	allocateLineDiscounts(items, subtotal, discount+membershipDiscount)
	total := subtotal - discount - membershipDiscount + shipping

	// 每人限次在 redeemCouponTx 内与核销同事务判定（行锁 + 计数），
	// 避免事务外先读再写被并发绕过。

	orderNo, err := s.newOrderNo(ctx, projectID)
	if err != nil {
		return nil, err
	}

	head := &ordermodel.OrderEntity{
		ProjectID:     projectID,
		OrderNo:       orderNo,
		Status:        ordermodel.OrderStatusPending,
		UserID:        userID,
		CustomerEmail: email,
		CustomerName:  strings.TrimSpace(req.CustomerName),
		CustomerPhone: strings.TrimSpace(req.CustomerPhone),
		Currency:      "CNY",
		Subtotal:      subtotal,
		DiscountTotal: discount,
		// 会员折扣与券各自独立计账（相加扣减）：这一列与 DiscountTotal 一起构成总扣减，
		// 而 Total 已经把它减掉了。两列分开存是为了保住 SEC-001 那条既有判据
		//（无优惠码时 DiscountTotal 恒为 0），见 order_membership_discount.go 的文件头。
		MembershipDiscountTotal: membershipDiscount,
		ShippingTotal:           shipping,
		TaxTotal:                0,
		Total:                   total,
		ShipName:                strings.TrimSpace(req.Shipping.Name),
		ShipPhone:               strings.TrimSpace(req.Shipping.Phone),
		ShipProvince:            strings.TrimSpace(req.Shipping.Province),
		ShipCity:                strings.TrimSpace(req.Shipping.City),
		ShipDistrict:            strings.TrimSpace(req.Shipping.District),
		ShipAddress:             strings.TrimSpace(req.Shipping.Address),
		ShipZip:                 strings.TrimSpace(req.Shipping.Zip),
		BillName:                strings.TrimSpace(req.Billing.Name),
		BillPhone:               strings.TrimSpace(req.Billing.Phone),
		BillProvince:            strings.TrimSpace(req.Billing.Province),
		BillCity:                strings.TrimSpace(req.Billing.City),
		BillDistrict:            strings.TrimSpace(req.Billing.District),
		BillAddress:             strings.TrimSpace(req.Billing.Address),
		BillZip:                 strings.TrimSpace(req.Billing.Zip),
		PaymentMethod:           strings.TrimSpace(req.PaymentMethod),
		PaymentMethodTitle:      strings.TrimSpace(req.PaymentMethodTitle),
		CreatedVia:              createdVia,
		IPAddress:               strings.TrimSpace(req.IPAddress),
		UserAgent:               strings.TrimSpace(req.UserAgent),
		RequestID:               strings.TrimSpace(req.RequestID),
		Remark:                  strings.TrimSpace(req.Remark),
		AdminNote:               strings.TrimSpace(req.AdminNote),
		CreateBy:                req.CreateBy,
		CreateTime:              now,
		UpdateTime:              now,
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
	// 工程作用域随端口下传（审计 DB-009）：没有它，换非超级角色后这次读取会静默返回 0 行，
	// 表现是「每个变体都查不到」⇒ 下单报「规格不存在」，而库里明明有。projectID 就是本函数
	// 的形参 —— 调用方（草稿 / 下单）已经带着它，不存在「拿不到工程」的情形。
	snapshots, err := s.product.VariantSnapshots(ctx, variantIDs, projectID)
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
			//
			// 端口已按工程作用域过滤（不属于本工程的变体根本不出现在快照里，见
			// VariantSnapshotPort 的契约），所以这里到不了 —— 留着当第二道防线：
			// 端口契约被改坏时仍然拦得住，而不是让订单落一行商品名为空的快照项。
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

// 成本快照的责任分工（迁移 256 之后）：
//
//	product 侧给出 *int64（nil = 该变体在归属仓尚未核算），order 侧原样落库 ——
//	order_items.cost_price 可空，NULL（没核算）与 0（赠品 / 内部划拨这类合法显式成本）
//	严格区分，绝不用 0 冒充未知。跨模块契约里不再有数量哨兵，消费方不必知道编码约定。

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
// **开号与否只看显式请求字段 req.ProvisionGuestAccount**（docs/02-W §4）：
// CreatedVia 由建单入口确定，只供订单审计使用，不决定客户是否开户。
// 三态语义见 dto.CreateOrderReq.ProvisionGuestAccount 的注释，其中：
//
//	· false → 明确不开号（后台代客建单页的默认档）；
//	· nil   → 调用方未表态，保持既有前台 checkout 行为（下单即开户）。
//
// 返回值二：只有「这次确实新建了账号、且初始密码寄出去了」才为 true。
// 邮箱已有账号时我们只关联、绝不改密码，此时告诉客户「密码已发到你邮箱」
// 会让他在邮箱里白找一场。
func (s *Service) ensureGuestAccount(ctx context.Context, req *orderdto.CreateOrderReq) (*uint64, bool) {
	if req.UserID != nil || s.guest == nil {
		return req.UserID, false
	}
	// 显式开关优先：明确说了不开号就到此为止（后台代客建单的默认档走这一支）。
	// nil 落到下面那一段 —— 那是既有 checkout 链路，行为与本次改动之前逐字一致。
	if req.ProvisionGuestAccount != nil && !*req.ProvisionGuestAccount {
		return req.UserID, false
	}
	gres, gerr := s.guest.EnsureGuestAccount(ctx, &usercontract.GuestAccountInput{
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
