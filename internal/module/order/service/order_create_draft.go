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

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	usercontract "go_wp/internal/module/user/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
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

	// 算钱用的三个输入（BIZ-11 抽出 applyLineMoney 后必须在草稿上留一份）：
	// 会员身份在事务内可能变化，那时要拿这三项重算折扣、分摊与总额。
	subtotal       int64
	couponDiscount int64
	shippingTotal  int64
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
// 步骤顺序：快照 → 券试算 → 会员身份预解析 → 会员折扣 → 分摊 → 订单号 → 订单头。
// 三处顺序有硬理由，改动前先读一遍：
//
//	· 券要等小计算出来才能试算；
//	· 会员身份排在会员折扣之前（BIZ-3 新增）：会员身份按账号解析，而访客的身份
//	  恰恰是这一单才建出来的账号；
//	· 会员折扣排在分摊之前：退款按分摊后的行实付算，折扣不进分摊就会退多。
//
// **BIZ-11**：开号本身已经移进建单事务（见 provisionGuestAccountTx），这里只做
// **只读**的身份预解析（resolveDraftUserID）—— 事务内拿到真实账号后若身份变了，
// 由 order_create.go 调 applyLineMoney 重算一次折扣与总额。
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

	// 会员身份：只用**事务前能确定**的那一份（调用方显式传入的登录身份，或按邮箱
	// 只读查到的既有账号）。新邮箱在这里还没有账号 —— 开号在事务内完成，
	// 那时若真的建出了账号，会有一次重算（见 applyLineMoney）。
	userID := s.resolveDraftUserID(ctx, req)

	// 会员折扣与分摊不在这里算：它们依赖**会员身份**，而身份的最终形态要等事务内的
	// 开号结果（BIZ-11）。草稿构造完之后由 applyLineMoney 统一算一次 ——
	// 事务内若开出了新账号、身份变了，就再算一次（同一份实现，不会漂移）。

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
		// 币种取全局默认（进程内缓存值，不查库）：币种是**标签**，金额仍是数值（分），
		// 按产品口径货币由后台全局限定为单值、前台不提供货币选择。
		//
		// 这一列是**下单当时的快照**（快照原则）：改配置只影响此后新建的订单，
		// 历史订单的 currency 不动 —— 订单详情要还原的是「当时是什么」。
		Currency: orderCurrency(),
		Subtotal: subtotal,
		// 会员折扣与总额由 applyLineMoney 统一填（见该方法的注释）：
		// 会员折扣与券各自独立计账（相加扣减），两列分开存是为了保住 SEC-001
		//（无优惠码时 DiscountTotal 恒为 0），见 order_membership_discount.go 的文件头。
		DiscountTotal: discount,
		ShippingTotal: shipping,
		TaxTotal:      0,
		ShipName:      strings.TrimSpace(req.Shipping.Name),
		ShipPhone:     strings.TrimSpace(req.Shipping.Phone),
		ShipProvince:  strings.TrimSpace(req.Shipping.Province),
		ShipCity:      strings.TrimSpace(req.Shipping.City),
		ShipDistrict:  strings.TrimSpace(req.Shipping.District),
		ShipAddress:   strings.TrimSpace(req.Shipping.Address),
		ShipZip:       strings.TrimSpace(req.Shipping.Zip),
		// 国家代码只做 TrimSpace，与同一批地址列一致：形状校验（两个 ASCII 字母、大写归一化）
		// 落在**收参处**（结算表单是客户端可控输入），落库层不重复一份规则。
		ShipCountry:        strings.TrimSpace(req.Shipping.Country),
		BillName:           strings.TrimSpace(req.Billing.Name),
		BillPhone:          strings.TrimSpace(req.Billing.Phone),
		BillProvince:       strings.TrimSpace(req.Billing.Province),
		BillCity:           strings.TrimSpace(req.Billing.City),
		BillDistrict:       strings.TrimSpace(req.Billing.District),
		BillAddress:        strings.TrimSpace(req.Billing.Address),
		BillZip:            strings.TrimSpace(req.Billing.Zip),
		BillCountry:        strings.TrimSpace(req.Billing.Country),
		PaymentMethod:      strings.TrimSpace(req.PaymentMethod),
		PaymentMethodTitle: strings.TrimSpace(req.PaymentMethodTitle),
		CreatedVia:         createdVia,
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

	d := &orderDraft{
		projectID:      projectID,
		head:           head,
		items:          items,
		appliedCoupon:  appliedCoupon,
		subtotal:       subtotal,
		couponDiscount: discount,
		shippingTotal:  shipping,
		now:            now,
	}
	// 会员折扣 + 逐行分摊 + 总额（唯一实现，事务内身份变化时会再调一次）。
	s.applyLineMoney(ctx, d, userID)
	return d, nil
}

// applyLineMoney 用给定会员身份算会员折扣、逐行分摊与订单总额（BIZ-11 抽出）。
//
// 为什么抽出来：开号移进建单事务之后，「算钱那一刻的会员身份」与「最终落库的身份」
// 可能在**同一个订单内**发生变化（事务内建出了新账号，或发现邮箱其实早有账号）。
// 抽出这一份实现，草稿阶段与事务内各调一次，两处不可能漂移 —— 复制一份到事务里
// 才是真正的资损风险（一处改了、另一处忘改）。
//
// 口径与拆分前逐字一致：
//   - 会员折扣落在**券之后**（相加扣减，上界夹在小计内，见 membershipDiscountAmount）；
//   - 落在**分摊之前**（退款按分摊后的行实付算，折扣不进分摊就会退多）；
//   - 独立计账：进 membership_discount_total，**不动** discount_total 的语义
//     （SEC-001「无优惠码时折扣恒为 0」）。
func (s *Service) applyLineMoney(ctx context.Context, d *orderDraft, userID *uint64) {
	if d == nil || d.head == nil {
		return
	}
	membership := s.resolveMembershipDiscount(ctx, d.projectID, userID, d.subtotal, d.couponDiscount)
	allocateLineDiscounts(d.items, d.subtotal, d.couponDiscount+membership)
	d.head.MembershipDiscountTotal = membership
	d.head.DiscountTotal = d.couponDiscount
	d.head.Total = d.subtotal - d.couponDiscount - membership + d.shippingTotal
}

// resolveDraftUserID 建单前对会员身份做**只读**预解析（BIZ-11）。
//
// 三态与开号的判定同源（见 provisionGuestAccountTx），区别只有一个：这里**不建号**。
// 为什么需要它：会员折扣要按既有身份算，而开号已经移进事务 —— 不预解析的话，
// 「未登录但邮箱早就是会员」的老客户会从「有会员价」静默变成「没有会员价」。
func (s *Service) resolveDraftUserID(ctx context.Context, req *orderdto.CreateOrderReq) *uint64 {
	if req == nil || req.UserID != nil || s.guest == nil {
		return req.UserID
	}
	if req.ProvisionGuestAccount != nil && !*req.ProvisionGuestAccount {
		return nil
	}
	if uid, found, err := s.guest.LookupGuestAccount(ctx, strings.TrimSpace(req.CustomerEmail)); err == nil && found && uid != 0 {
		return &uid
	}
	return nil
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

// provisionGuestAccountTx 访客开号（BIZ-11：在**建单事务内**执行）。
//
// 三态判定与拆分前逐字一致（显式开关 req.ProvisionGuestAccount，见 dto 的注释），
// 只有两点不同：
//
//  1. 建号写进**调用方的事务** —— 订单回滚时账号一起回滚。原来开号在事务之外，
//     建号成功而订单因库存不足回滚，就留下一个能登录却没有订单的孤儿账号；
//  2. **不在事务内发信**（发了回滚收不回）：邮件载荷原样返回给调用方，
//     由它在事务提交后调 SendGuestAccountMail。
//
// 开号失败**不阻断下单**（既有口径）：订单是主体、账号是附赠能力 —— 出错时记日志
// 并继续，user_id 留空（客户仍可用这个邮箱走「忘记密码」自己开号）。
//
// 邮箱已有账号时只关联、**绝不改密码** —— 那条安全边界在 user 模块里守着。
func (s *Service) provisionGuestAccountTx(ctx context.Context, tx *gorm.DB, req *orderdto.CreateOrderReq) (*uint64, *usercontract.GuestAccountMail, error) {
	if req == nil || req.UserID != nil || s.guest == nil {
		return req.UserID, nil, nil
	}
	// 显式开关优先：明确说了不开号就到此为止（后台代客建单的默认档走这一支）。
	// nil 落到下面那一段 —— 那是既有 checkout 链路，行为与本次改动之前逐字一致。
	if req.ProvisionGuestAccount != nil && !*req.ProvisionGuestAccount {
		return req.UserID, nil, nil
	}
	gres, gerr := s.guest.EnsureGuestAccountTx(ctx, tx, &usercontract.GuestAccountInput{
		Email:      strings.TrimSpace(req.CustomerEmail),
		Name:       strings.TrimSpace(req.CustomerName),
		Locale:     req.Locale,
		RegisterIP: req.IPAddress,
	})
	if gerr != nil || gres == nil || gres.UserID == 0 {
		if gerr != nil {
			logger.Scene("order").Error(gerr, "访客开号失败（不阻断下单）")
		}
		return req.UserID, nil, nil
	}
	id := gres.UserID
	return &id, gres.PendingMail, nil
}

// orderCurrency 新建订单的币种标签。
//
// 币种是**标签**：金额本来就是数值（分），改币种只改标签 —— 按产品口径货币由后台
// 全局限定为单值、前台不提供货币选择（用户只能改自己的地区）。
// 读的是 pkg/i18n 的进程内缓存值（装配期载入、tick 与保存后刷新），不在请求路径查库。
//
// 注意：orders.currency 是**下单当时的快照**，本函数只在建单时取值 ——
// 历史订单的币种不随配置变化（订单详情要还原「当时是什么」）。
func orderCurrency() string {
	if v := strings.TrimSpace(i18n.GetDefaultCurrency()); v != "" {
		return v
	}
	return "CNY"
}
