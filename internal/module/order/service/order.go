package orderservice

// 事务边界：订单头 + 订单项 + 流转流水 + 券核销 + 扣库存全部落在 persistOrderTx 同一个事务里。
// 跨模块只传 *gorm.DB 句柄（stock.DeductStockTx），库存侧不再自己开事务。
// 补偿失败时库里会同时留下「有订单」和「库存没动」，同库跨模块必须一起回滚。
// 建单里藏过两个资损缺陷：无券时采信客户端折扣、每人限次在事务外判定。

//
// 这一段的共同点是**只读**：商品事实来自 product 契约，优惠码试算是纯读，
// 归因只是把请求里的快照转成 JSONB，访客开号失败也不阻断下单。
// 把它们与事务写入分开，是为了让「钱怎么算出来的」这件事可以单独读、单独测 ——
// 金额与折扣此前挤在 CreateOrder 的中段，正是审计里两个资损缺陷的藏身处。

//
// 一次建单有四处数据库写入：订单头、订单项、状态流水、券核销（含券 usedCount），
// 外加跨模块的库存扣减。它们必须同生共死：
//   · 任一步失败 → **整单不存在**，不留「有单没扣库存」的待付款僵尸单，
//     也不留「券核销了但没下单」；
//   · 库存不足 / 库存服务不可用都是业务结论，直接把错误返回给调用方，
//     不再走「先建单、再补偿成已取消」——同库跨模块不再用补偿，只传事务句柄
//     （DeductStockTx，先例 masterdata.RecordChangesTx）。

//
// 口径（已拍板，不是这里能改的）：**会员折扣与券各自独立计账、相加扣减**。
// 落到 orders 上是两列：
//
//	discount_total           —— 券折扣（既有语义**一个字节都不动**）
//	membership_discount_total —— 会员折扣（迁移 462 已加好，本文件开始真正写入）
//
// 为什么不并进 discount_total：SEC-001 有一条既有判据「无优惠码时折扣恒为 0」
// （见 resolveCoupon 的注释）。把会员折扣算进同一列，那条判据就会在每次会员下单时
// 出现常规例外，审计与回归测试都要重新解释「这一列到底代表什么」。
//
// 三条边界：
//   · **只读**：Reader.Resolve 是只读路径，本文件不写库、不建归属行；
//   · **失败不阻断下单**：会员服务读不到时降级为「不打折 + 日志」，不是拒单
//     （会员是附加能力，订单是主体 —— 与访客开号失败不阻断下单同一取舍）；
//   · **绝不放大到负数**：折扣合计夹在小计之内（见 resolveMembershipDiscount）。

//
// 「谁去扣钱」不在订单域：网关（PayPal / Stripe / 微信支付）是外部系统，订单域只负责
// 「钱到了之后把这一单记成已付款」。这条边界让支付通道可以换、可以加、可以在测试里换成
// 假实现，而订单表与状态机一个字都不用改。
//
// 幂等是这一层最重要的性质：网关的异步通知会重发、访客会连点两次下单按钮、
// 上层会失败重试。重复到达时**必须返回和第一次相同的结果**，而不是报「状态不支持」——
// 报错会让网关一直重试，也让用户的第二次点击变成一次报错弹窗。

//
// 状态机用**表驱动**而不是一串 if：全部合法边能一眼看全，加状态时不会漏改某处判断。
// 漏掉的判断不会报错，只会让某条路径永远走不通、或者误放行一条不该有的边 ——
// 两种都是事故。

//
// 备注是「人对这张单的判断」：客服记下客户说了什么、仓库记下为什么改地址。
// 它**不是状态流转**，所以不写 status_logs —— 那条链回答的是「订单处在哪一步、什么时候变过」，
// 把备注变更混进去，会让「这单什么时候发的货」变成要翻记录才能看出来。

//
// 建单成功即扣库存，待付款单若长期不支付也不取消，可用量会被一直占住。
// 定时扫描超过 TTL 的 pending 单，复用 CancelOrder（归还库存 + 释放券核销）。

//
// 端口由**消费方**（product 模块）声明（productcontract.PurchaseChecker），
// 实现在这里 —— 同型先例是 membershipcontract.PurchaseSource 的接入方式。

//
// 契约在 **membership 侧**（membershipcontract.PurchaseSource），实现放在订单侧：
// 「什么算消费」这个问题只有订单域能回答（哪些状态算钱进来了、金额取哪一列），
// 而 membership 模块读不到 users 表、也不该读 orders 表（AGENTS.md 的表隔离）。
//
// 形状是**批量**的（一次给全工程的映射），不是逐用户查询：membership 的日结重算
// 要的是「谁该升级」，它无从枚举 userID —— 只有订单侧能给出这份清单。
// 逐用户调 CustomerOrderSummaryOf 在这里是错的：那是 N+1
//（几千个客户 = 几千次往返 + 几千个作用域事务）。
//
// 这是一个**只读**端口：接口里只有一条返回映射的方法，没有写入能力。

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"
	"go_wp/pkg/database"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
	"go_wp/pkg/utils"
)

const (
	// maxOrderItems 单笔订单的商品项上限。
	//
	// 上限不是性能问题而是**幂等与计算**问题：订单号与金额都在一次请求里算完，
	// 无上限的输入会让一次请求锁住任意多的库存行。
	maxOrderItems = 100
	// maxItemQuantity 单项数量上限（与库存的硬上限同量级）。
	maxItemQuantity = 100000
)

// CreateOrder 访客结算建单，来源固定为 checkout。
func (s *Service) CreateOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaCheckout)
}

// CreateAPIOrder 站点 API 建单，来源固定为 api。
func (s *Service) CreateAPIOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaAPI)
}

// CreateAdminOrder 后台代客建单，来源固定为 admin。
func (s *Service) CreateAdminOrder(ctx context.Context, req *orderdto.CreateOrderReq) (res *orderdto.CreateOrderResp, err error) {
	return s.createOrder(ctx, req, ordermodel.CreatedViaAdmin)
}

// createOrder 共用建单流水：校验 → 幂等 → 备料 → 落库 → 扣库存。
func (s *Service) createOrder(ctx context.Context, req *orderdto.CreateOrderReq, createdVia string) (res *orderdto.CreateOrderResp, err error) {
	if err = validateCreateOrderReq(req); err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(req.ProjectID)

	// 幂等：同一 request_id 命中既有单就原样返回，绝不再扣一次库存。
	existing, err := s.orderByRequestID(ctx, projectID, req.RequestID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	// 备料：商品事实、金额、优惠码、归因 —— 全部在事务之外算好。
	// （访客开号**不在这里**：BIZ-11 把它移进了下面的事务。）
	draft, err := s.buildOrderDraft(ctx, req, projectID, createdVia)
	if err != nil {
		return nil, err
	}

	// 订单（头 + 项 + 流水 + 券核销 + 扣库存）与**访客开号**同一个事务：
	// 任一步失败整单不存在，账号也随之不存在 —— 不再有「建号成功、订单回滚」
	// 留下的孤儿账号（客户能登录、却没有任何订单，还已经收到了初始密码邮件）。
	//
	// 初始密码邮件**不在这里发**（事务还没提交，发了回滚收不回）：载荷带出来，
	// 提交成功之后再发；发信失败只影响响应里的 AccountMailed，不回滚任何东西。
	var pendingMail *usercontract.GuestAccountMail
	if err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		userID, mail, perr := s.provisionGuestAccountTx(ctx, tx, req)
		if perr != nil {
			return perr
		}
		if !sameUserID(userID, draft.head.UserID) {
			// 会员身份与事务前预解析的不一致（通常是事务内刚建出了账号）：
			// 用真实身份重算会员折扣、逐行分摊与总额，再落库。
			draft.head.UserID = userID
			s.applyLineMoney(ctx, draft, userID)
		}
		pendingMail = mail
		return s.persistOrderTx(ctx, tx, draft)
	}); err != nil {
		// 并发同键撞唯一索引：另一个请求已经把单建出来了，回读它原样返回。
		if database.IsUniqueViolation(err) {
			if dup, derr := s.orderByRequestID(ctx, projectID, req.RequestID); derr == nil && dup != nil {
				return dup, nil
			}
		}
		return nil, err
	}
	// 事务已提交：这时才发初始密码邮件。发不出去不是下单失败 —— 账号已经在了，
	// 客户可以走「忘记密码」自己重置；这里只影响响应里的 AccountMailed。
	if pendingMail != nil && s.guest != nil && s.guest.SendGuestAccountMail(ctx, pendingMail) == nil {
		draft.accountMailed = true
	}

	return draft.response(), nil
}

// sameUserID 比较两个可空会员身份（指针本身不参与比较，比的是它指向的 id）。
func sameUserID(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// validateCreateOrderReq 入参校验：不碰数据库，纯形状检查。
func validateCreateOrderReq(req *orderdto.CreateOrderReq) error {
	if req == nil {
		return errors.New(orderenums.ErrInvalidParam)
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return errors.New(orderenums.ErrProjectRequired)
	}
	email := strings.TrimSpace(req.CustomerEmail)
	if email == "" {
		return errors.New(orderenums.ErrCustomerEmailRequired)
	}
	if !strings.Contains(email, "@") || len(email) < 3 {
		return errors.New(orderenums.ErrCustomerEmailInvalid)
	}
	if len(req.Items) == 0 {
		return errors.New(orderenums.ErrItemsRequired)
	}
	if len(req.Items) > maxOrderItems {
		return errors.New(orderenums.ErrItemLimitExceeded)
	}
	return nil
}

// orderByRequestID 幂等键命中则返回既有单的响应；无键或未命中返回 nil。
//
// 首次查（进入建单前）与唯一冲突后的回读共用它，保证两条路径返回**同一份形状**
// （Duplicated=true 的响应），不会一条带 accountMailed、另一条不带。
func (s *Service) orderByRequestID(ctx context.Context, projectID, requestID string) (*orderdto.CreateOrderResp, error) {
	reqID := strings.TrimSpace(requestID)
	if reqID == "" {
		return nil, nil
	}
	existing, err := s.orders.GetByRequestID(ctx, projectID, reqID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	return &orderdto.CreateOrderResp{
		ID: existing.ID, OrderNo: existing.OrderNo, Status: existing.Status,
		Total: existing.Total, Currency: existing.Currency, Duplicated: true,
	}, nil
}

// newOrderNo 生成订单号：时间前缀 + 随机后缀。
//
// 时间前缀便于人工识别与归档（客服报单号时能看出是哪天），随机后缀避免同秒碰撞；
// 唯一约束兜底，撞了就重试（概率极低，但重试比报错好）。
func (s *Service) newOrderNo(ctx context.Context, projectID string) (no string, err error) {
	const attempts = 5
	buf := make([]byte, 4)
	for i := 0; i < attempts; i++ {
		if _, rerr := rand.Read(buf); rerr != nil {
			return "", rerr
		}
		no = fmt.Sprintf("GWP%s%08X", time.Now().Format("20060102"), uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
		existing, gerr := s.orders.GetByNo(ctx, projectID, no)
		if gerr != nil {
			return "", gerr
		}
		if existing == nil {
			return no, nil
		}
	}
	return "", errors.New(orderenums.ErrOrderNoTaken)
}

// operatorTypeOf 下单入口 → 操作人类型。
func operatorTypeOf(createdVia string) string {
	if createdVia == ordermodel.CreatedViaAdmin {
		return ordermodel.OperatorTypeAdmin
	}
	return ordermodel.OperatorTypeCustomer
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// centsToYuanLabel 分 → 元的展示串（仅用于备注与提示，不参与计算）。
func centsToYuanLabel(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

// marshalAttribution 归因快照 → JSONB。
//
// nil 表示没有访客上下文（后台代客下单没有追踪数据），落空对象而不是 NULL ——
// 列是 NOT NULL，且「没有归因数据」与「这一列不存在」对下游是两件事。
func marshalAttribution(a *orderdto.Attribution) (json.RawMessage, error) {
	if a == nil {
		return json.RawMessage("{}"), nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

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
// 由 createOrder 调 applyLineMoney 重算一次折扣与总额。
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
		//（无优惠码时 DiscountTotal 恒为 0），见 resolveMembershipDiscount。
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

// persistOrderTx 建单写入的**事务内实现**（tx 由调用方负责提交 / 回滚）。
//
// 回填 head.ID：订单项与流水都要挂这个 id，而它是 CreateTx 之后才有的。
func (s *Service) persistOrderTx(ctx context.Context, tx *gorm.DB, d *orderDraft) error {
	if cerr := s.orders.CreateTx(ctx, tx, d.head); cerr != nil {
		return cerr
	}
	for _, it := range d.items {
		it.OrderID = d.head.ID
	}
	if cerr := s.items.CreateBatchTx(ctx, tx, d.items); cerr != nil {
		return cerr
	}
	if lerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
		OrderID:      d.head.ID,
		FromStatus:   "",
		ToStatus:     ordermodel.OrderStatusPending,
		OperatorType: operatorTypeOf(d.head.CreatedVia),
		OperatorID:   d.head.CreateBy,
		Remark:       "建单",
		CreateTime:   d.now,
	}); lerr != nil {
		return lerr
	}
	if rerr := s.redeemCouponTx(ctx, tx, d.appliedCoupon, d.head.ID, d.head.OrderNo, d.head.DiscountTotal, d.head.UserID, d.now); rerr != nil {
		return rerr
	}
	// 扣库存：同库跨模块，把**订单事务的句柄**传给库存的 …Tx 方法。
	// 不足 / 不可用都让整个事务回滚 —— 订单、项、流水、券核销一起消失。
	return s.deductStockTx(ctx, tx, d)
}

// deductStockTx 建单出库（在订单事务内）：库存变动落在调用方的事务里，失败原样反馈。
//
// 区分「库存不足」与「库存服务不可用」：前者是客户看得到答案的业务结论，
// 后者是运维要看的问题，两者在错误文案与后续排查上完全不同。
func (s *Service) deductStockTx(ctx context.Context, tx *gorm.DB, d *orderDraft) error {
	lines := make([]ordercontract.StockLine, 0, len(d.items))
	for _, it := range d.items {
		lines = append(lines, ordercontract.StockLine{
			ProductID: it.ProductID,
			VariantID: it.VariantID,
			SKUCode:   it.SKU,
			Quantity:  it.Quantity,
		})
	}
	dErr := s.stock.DeductStockTx(ctx, tx, &ordercontract.StockDeduction{
		ProjectID:  d.projectID,
		ReasonCode: "sale_out",
		SourceType: "order",
		SourceRef:  d.head.OrderNo,
		Remark:     "订单出库",
		Lines:      lines,
	})
	if dErr == nil {
		return nil
	}
	reason := orderenums.ErrStockInsufficient
	msg := strings.ToLower(dErr.Error())
	if !strings.Contains(msg, "不足") && !strings.Contains(msg, "insufficient") {
		reason = orderenums.ErrStockUnavailable
	}
	return errors.New(reason)
}

// allocateLineDiscounts 把订单级折扣按行小计比例分摊到各订单项。
//
// 分摊的目标有两条，**同时**成立才算分对：
//
//  1. 各行 LineDiscount 之和等于实际吃掉的折扣（退款按 LineTotal 算，
//     避免「券减在头上、退按原价」的资损）；
//  2. 任何一行都不得被折扣吃穿 —— LineTotal >= 0（BIZ-08）。
//
// 第 2 条不是理论洁癖：末行无条件吸收舍入差会溢出小计。复算用例：3 行小计
// 34/33/33（subtotal=100 分）+ fixed=99 分券 → 前两行 floor 分配 33/32（allocated=65）
// → 末行 99-65=34 > 33 ⇒ 旧实现给出 LineTotal = -1，部分退货于是显示「应退 -0.01 元」。
//
// 做法：末行优先吸收舍入差；**溢出时不硬塞**，而是把溢出额回摊到前面的行
// （从最后一行往前，逐行填到它的剩余额度为止）。回摊顺序从后往前与「末行吸收」
// 同一方向，逐行填满即停，结果与入参顺序、与浮点无关（全整数运算，确定性）。
//
// 折扣总额超过小计之和时（建单侧已夹取，这里独立守住口径）仍会剩下填不完的额度，
// 直接丢弃：宁可少减折扣，也不能产生负的行实付 —— 后者会在退款链路上变成凭空补偿。
func allocateLineDiscounts(items []*ordermodel.OrderItemEntity, subtotal, discount int64) {
	if len(items) == 0 {
		return
	}
	if discount <= 0 || subtotal <= 0 {
		for _, it := range items {
			it.LineDiscount = 0
			it.LineTotal = it.LineSubtotal
		}
		return
	}
	last := len(items) - 1
	var allocated int64
	for i, it := range items {
		if i == last {
			continue // 末行最后单独算（先看它能不能吃下剩余的舍入差）
		}
		it.LineDiscount = discount * it.LineSubtotal / subtotal
		if it.LineDiscount > it.LineSubtotal {
			// 防御：比例分配在 subtotal > 各行小计之和时可能溢出（口径异常，不静默吃穿）。
			it.LineDiscount = it.LineSubtotal
		}
		allocated += it.LineDiscount
	}

	rest := discount - allocated
	if rest < 0 {
		rest = 0
	}
	items[last].LineDiscount = min64(rest, items[last].LineSubtotal)
	// 末行吃不下的部分回摊到前面的行（从后往前，逐行填到它的剩余额度为止）。
	deficit := rest - items[last].LineDiscount
	for i := last - 1; i >= 0 && deficit > 0; i-- {
		room := items[i].LineSubtotal - items[i].LineDiscount
		if room <= 0 {
			continue
		}
		add := min64(room, deficit)
		items[i].LineDiscount += add
		deficit -= add
	}

	for _, it := range items {
		// 收口：任何一行都不得为负（回摊与比例分配都已夹取，这里是最后一道）。
		if it.LineDiscount < 0 {
			it.LineDiscount = 0
		}
		if it.LineDiscount > it.LineSubtotal {
			it.LineDiscount = it.LineSubtotal
		}
		it.LineTotal = it.LineSubtotal - it.LineDiscount
	}
}

// min64 取较小值（分摊里的夹取用，避免为两行引入 math.Min）。
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// lineRefundAmount 按分摊后的行实付额计算部分退货应退金额（分）。
func lineRefundAmount(it *ordermodel.OrderItemEntity, qty int) int64 {
	if it == nil || qty <= 0 {
		return 0
	}
	if it.Quantity <= 0 {
		return 0
	}
	return it.LineTotal * int64(qty) / int64(it.Quantity)
}

// SetMembershipReader 注入会员身份读取端口（装配期调用；**可缺**）。
//
// 可缺的语义是「会员折扣功能未开启」：未注入时折扣恒为 0，订单金额与本批接入前
// **逐字一致**。这与 runtimefragment 的 sitePageResolver 同一口径 ——
// 缺能力就按「没有这项能力」算，而不是报错或静默算成别的数。
func (s *Service) SetMembershipReader(reader membershipcontract.Reader) {
	if s == nil {
		return
	}
	s.membership = reader
}

// membershipDiscountAmount 纯函数：按扣减百分比算会员折扣额（分）。
//
// 三条规则：
//  1. 百分比按 kind=discount 的取值域 1..100 解释（20 = 打八折），
//     越界值一律按边界夹住 —— 迁移 462 的 CHECK 已经在库上拦过一次，
//     这里是「万一有人绕过应用层写库」时的第二道，免得负折扣被算成加价；
//  2. 基数 = **小计**（与券同基数）：两套折扣各自独立计账，谁也不以对方的结果为基数
//     —— 那会让「先加券再打折」与「先打折再加券」得到不同金额，而运营在后台
//     看到的是两个独立开关，没有理由认为它们有先后；
//  3. 上界 = 小计扣掉券之后**还剩多少**（remaining）：相加扣减下两道折扣可能合计超过小计
//     （券 100% + 会员 50%），不夹住就会出现「应付为负」（负数总额没有意义，
//     而且它会顺着 total 传到支付金额上）。
func membershipDiscountAmount(subtotal, remaining, percent int64) int64 {
	if percent <= 0 || subtotal <= 0 || remaining <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	amount := subtotal * percent / 100
	if amount > remaining {
		amount = remaining
	}
	if amount < 0 {
		return 0
	}
	return amount
}

// resolveMembershipDiscount 取该访客在本工程的会员折扣额（分）。
//
// 返回值的三种形态都是**明确的结论**，不是「失败与 0 混在一起」：
//   - 未注入端口 / 无账号 / 无折扣权益 → 0（正常路径，不打日志）；
//   - 解析失败（默认等级缺失、库不可用）→ 0 **加一条 Warn**（降级方向是照收钱，
//     不是免单：一次数据库抖动不该让全站订单集体打折，那正是「错误被解释成优惠」的资损形态）；
//   - 有折扣权益 → 按小计算出的金额，并夹在小计减去券之后的余额内。
//
// userID 为 nil / 0 时**不查库**直接返回 0：访客还没有账号就没有会员身份，
// 为它跑一次解析只是白花一次数据库往返（AGENTS.md 不变量 1 的取舍）。
func (s *Service) resolveMembershipDiscount(ctx context.Context, projectID string, userID *uint64, subtotal, couponDiscount int64) (discount int64) {
	if s.membership == nil || userID == nil || *userID == 0 {
		return 0
	}
	if strings.TrimSpace(projectID) == "" {
		return 0
	}
	member, err := s.membership.Resolve(ctx, &membershipdto.ResolveReq{
		ProjectID: projectID,
		UserID:    *userID,
	})
	if err != nil {
		logger.Scene("order").
			With("projectId", projectID).With("userId", *userID).
			Warn("会员身份解析失败，本单按无会员折扣建单（折扣降级为 0，不拒单）")
		return 0
	}
	if member == nil {
		return 0
	}
	remaining := subtotal - couponDiscount
	if remaining < 0 {
		remaining = 0
	}
	return membershipDiscountAmount(subtotal, remaining, member.DiscountPercent)
}

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
	// paidEvent 在事务内捕获、在事务**提交之后**派发（通知不是事务的一部分）。
	// 只有真正发生 pending → paid 跃迁的那一次才非 nil —— 见文件末尾的幂等说明。
	// 定位跳（DB-009 第四批）：支付回调只带订单 id（或商户单号），而 orders 带 FORCE
	// 策略 —— 没有作用域时这条加锁读拿不到行，回调会被当成「订单不存在」。
	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return nil, perr
	}
	var paidEvent *OrderPaidEvent
	err = s.orders.Transaction(ctx, func(tx *gorm.DB) error {
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
			// 支付通道可能在取消/退款之后才回调成功：钱已扣，不能对通道报错
			// （否则无限重试），也不能静默改状态。记流水 + 返回待人工核对。
			remark := defaultString(remark, "支付成功但订单已终态，待人工核对")
			if txnID != "" {
				remark += "（流水号 " + txnID + "）"
			}
			if cerr := s.logs.CreateTx(ctx, tx, &ordermodel.OrderStatusLogEntity{
				OrderID:      e.ID,
				FromStatus:   e.Status,
				ToStatus:     e.Status,
				OperatorType: defaultString(req.OperatorType, ordermodel.OperatorTypeSystem),
				OperatorID:   req.OperatorID,
				OperatorName: strings.TrimSpace(req.OperatorName),
				Remark:       remark,
				CreateTime:   now,
			}); cerr != nil {
				return cerr
			}
			res.ID, res.OrderNo, res.Status = e.ID, e.OrderNo, e.Status
			res.TransactionID = txnID
			res.NeedsManualReview = true
			return nil
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
		if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
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
		paidEvent = &OrderPaidEvent{
			OrderID:       e.ID,
			OrderNo:       e.OrderNo,
			Status:        ordermodel.OrderStatusPaid,
			Currency:      e.Currency,
			Total:         e.Total,
			Subtotal:      e.Subtotal,
			DiscountTotal: e.DiscountTotal,
			ShippingTotal: e.ShippingTotal,
			TaxTotal:      e.TaxTotal,
			PaymentMethod: method,
			TransactionID: txnID,
			PaidAt:        now,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 对外通知（best-effort）：事务已提交才派发，失败只记日志。
	//
	// **只有真正发生状态跃迁的那一次才派发**：上面三个幂等分支（重复的网关回调、
	// 用户双击、上层重试）若也通知，对端会重复发货、重复记账 ——
	// 那已经不是「通知」而是事故。这正是把 paidEvent 留在跃迁分支里赋值的原因。
	if paidEvent != nil {
		s.dispatchEvent(ctx, webhookcontract.EventOrderPaid, paidEvent)
	}
	return res, nil
}

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
		//
		// 归还量扣掉**已实际收货入库**的部分（BIZ-02）：客户已经退回来的货早就加进库存了，
		// 这里再按订单项原始数量全额归还一次，库存就凭空多出已退的那些件 —— 不报错，只多货。
		if rerr := s.restockUnreturnedTx(ctx, tx, e, "取消订单归还库存："+reason); rerr != nil {
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

// RefundOrder 后台退款入口：改状态 + 记支付流水号；**未发货时归还库存**（BIZ-07 方案 A）。
//
// 为什么未发货也要归还：同一张未发货订单上，「取消」与「退款」是两个按钮、两种结果
// —— 取消会把货还回仓库、退款不会。货从未出库，钱退了货自然还在库里，
// 两个按钮给出不同的库存结果只会让运营在不知情的情况下把库存记错。
//
// 已发货（shipped / completed）**一律不归还**：货已经出库，要回来必须经退货入库
// （有实物验收环节），凭空加回来等于把「系统里的数」当成「仓库里的货」。
func (s *Service) RefundOrder(ctx context.Context, req *orderdto.RefundOrderReq) (err error) {
	if req == nil || req.OrderID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	// 定位跳（DB-009 第四批）：同 ChangeStatus —— 请求只给订单 id。
	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return perr
	}
	return s.orders.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectID); serr != nil {
			return serr
		}
		return s.refundOrderTx(ctx, tx, projectID, req, true)
	})
}

// refundOrderTx 退款的事务体（句柄由调用方给，事务边界归调用方）。
//
// restock 控制「未发货时是否归还库存」，两个调用方各有明确理由：
//   - RefundOrder（后台直接退款）传 true —— 见它自己的注释；
//   - 退货单退款（refundReturnTx）传 false —— 货已经由退货入库那一步归还过，
//     这里再归还一次就是把同一批货加两遍。
func (s *Service) refundOrderTx(ctx context.Context, tx *gorm.DB, projectID string,
	req *orderdto.RefundOrderReq, restock bool) error {
	now := time.Now()
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
	// 归还判定用的是**加锁读到的当前状态**：调用方传来的 restock 只表达「这条路径该不该还」，
	// 「这一单该不该还」由订单自己回答。状态必须在改动之前取 —— 改完就成了 refunded，
	// 那时再看状态，未发货的单会被误判成「已发货、不归还」。
	unshipped := e.Status == ordermodel.OrderStatusPaid
	fields := map[string]any{"status": ordermodel.OrderStatusRefunded, "update_time": now}
	if tid := strings.TrimSpace(req.TransactionID); tid != "" {
		fields["transaction_id"] = tid
	}
	if uerr := s.orders.UpdateFieldsTx(ctx, tx, e.ProjectID, e.ID, fields); uerr != nil {
		return uerr
	}
	// 未发货的退款把货按「还没归还的数量」还回仓库（与取消同一条路径、同一本账）。
	if restock && unshipped {
		if rerr := s.restockUnreturnedTx(ctx, tx, e, "退款归还库存："+strings.TrimSpace(req.Reason)); rerr != nil {
			return rerr
		}
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
}

// restockUnreturnedTx 事务内把「还没归还的」订单项加回仓库 —— 取消订单与未发货退款共用。
//
// 归还量扣掉**已实际收货入库**的数量（BIZ-02）：部分退货已经把那些货加回来过，
// 再按订单项原始数量全额归还就是凭空多出库存。全部项都已归还时**不调用**库存变动 ——
// 「归还 0 件」没有业务含义，而且那条调用本身也是噪音（库存服务的判据不该由订单侧去踩）。
//
// 加锁顺序：本函数只锁订单（调用方已持锁），不碰退货单 —— 与 admitReceive
// 「先退货单、后订单」的顺序一致，不会形成环。
func (s *Service) restockUnreturnedTx(ctx context.Context, tx *gorm.DB, e *ordermodel.OrderEntity, remark string) error {
	items, ierr := s.items.ListByOrderIDTx(ctx, tx, e.ID)
	if ierr != nil {
		return ierr
	}
	if len(items) == 0 {
		return errors.New(orderenums.ErrOrderHasNoItems)
	}
	received, rerr := s.receivedByItemTx(ctx, tx, e.ProjectID, e.ID, items)
	if rerr != nil {
		return rerr
	}
	lines := restockLinesFor(items, received)
	if len(lines) == 0 {
		return nil
	}
	return s.stock.ChangeStockTx(ctx, tx, &ordercontract.StockAdjustment{
		ProjectID:  e.ProjectID,
		ReasonCode: "return_in",
		SourceType: "order",
		SourceRef:  e.OrderNo,
		Remark:     remark,
		Lines:      lines,
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

// maxAdminNoteLen 备注长度上限（与列宽一致，超了直接拒绝而不是静默截断）。
const maxAdminNoteLen = 500

// UpdateOrderNote 改订单的后台备注。
func (s *Service) UpdateOrderNote(ctx context.Context, req *orderdto.UpdateOrderNoteReq) (res *orderdto.OrderResp, err error) {
	if req == nil || req.OrderID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	note := strings.TrimSpace(req.AdminNote)
	if len(note) > maxAdminNoteLen {
		return nil, errors.New(orderenums.ErrNoteTooLong)
	}
	// 定位跳（DB-009 第四批）：后台备注入口只给订单 id。
	projectID, perr := s.locateOrderProject(ctx, req.OrderID)
	if perr != nil {
		return nil, perr
	}
	head, err := s.orders.GetByID(ctx, req.OrderID, projectID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	if err = s.orders.UpdateFields(ctx, head.ProjectID, head.ID, map[string]any{
		"admin_note":  note,
		"update_time": time.Now(),
	}); err != nil {
		return nil, err
	}
	updated, err := s.orders.GetByID(ctx, head.ID, head.ProjectID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, errors.New(orderenums.ErrOrderNotFound)
	}
	return toOrderResp(updated), nil
}

const (
	// pendingOrderTTL 待付款超时阈值：超过此时间未支付则自动取消。
	pendingOrderTTL = 30 * time.Minute
	// pendingOrderExpiryInterval 后台扫描间隔。
	pendingOrderExpiryInterval = 15 * time.Minute
	// pendingOrderExpiryBatch 单次扫描最多处理条数，避免一次拖住进程太久。
	pendingOrderExpiryBatch = 100
)

// ExpirePendingOrders 取消创建时间早于 cutoff 的待付款订单，返回成功取消条数。
//
// 逐工程扫描（DB-009 第三批）：orders 带 FORCE 策略，不带作用域的「全表扫描」在换
// 非超级角色后静默返回空集 —— 定时任务照样每 15 分钟跑一次，一单都不会取消，
// 库存被一直占住而日志里没有任何异常。工程清单由 model 从 projects 表取（那里是
// 隔离主体、不受策略约束），逐个工程各设一次作用域。
// 连工程都取不到时显式失败：静默返回 0 会把「读不到工程表」伪装成「没有超时订单」。
func (s *Service) ExpirePendingOrders(ctx context.Context, olderThan time.Duration, batchSize int) (expired int, err error) {
	if olderThan <= 0 {
		olderThan = pendingOrderTTL
	}
	if batchSize <= 0 {
		batchSize = pendingOrderExpiryBatch
	}
	// 工程清单：优先经注入的 project 契约，未注入时回退到 projects 表的只读兜底
	//（见 SetProjects；装配落点见 DB-009 第四批报告）。
	projectIDs, perr := s.projectIDs(ctx)
	if perr != nil {
		return 0, perr
	}
	cutoff := time.Now().Add(-olderThan)
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		list, lerr := s.orders.ListPendingCreatedBefore(ctx, projectID, cutoff, batchSize)
		if lerr != nil {
			return expired, lerr
		}
		for _, o := range list {
			if ctx.Err() != nil {
				return expired, nil
			}
			_, cerr := s.CancelOrder(ctx, &orderdto.CancelOrderReq{
				OrderID:      o.ID,
				ProjectID:    projectID,
				Reason:       "待付款超时自动取消",
				OperatorType: ordermodel.OperatorTypeSystem,
			})
			if cerr != nil {
				logger.Scene("order").With("order_id", o.ID).With("order_no", o.OrderNo).
					Warn("待付款超时取消失败（下轮重试）: " + cerr.Error())
				continue
			}
			expired++
		}
	}
	if expired > 0 {
		logger.Scene("order").With("expired", expired).Info("待付款超时订单已自动取消")
	}
	return expired, nil
}

// StartPendingOrderExpiryScheduler 启动后台定时扫描（进程内 goroutine，失败不 panic）。
func StartPendingOrderExpiryScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _ = svc.ExpirePendingOrders(ctx, pendingOrderTTL, pendingOrderExpiryBatch)
		}
		run()
		ticker := time.NewTicker(pendingOrderExpiryInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// 编译期断言：本实现满足商品侧声明的购买事实端口。
var _ productcontract.PurchaseChecker = (*Service)(nil)

// HasPurchasedProduct 判定某访客在本工程下是否买过某商品（只读）。
//
// 两条入口校验（不信任调用方）：
//   - userID 为 0 = 没有账号 → 直接 false 且不查库。0 不是有效账号 id（users.id 从 1 起）；
//     「没账号的人买过东西」这个命题本身不成立 —— 访客下单会经 guest 端口开号。
//   - productID 必须是合法 uuid：order_items.product_id 是 uuid 列，非法值会让 PG 报
//     "invalid input syntax for type uuid"，而那条错误在调用方看来像数据库故障、不像参数错误。
//     这里返回 false 而不是 error：不存在这样的商品 id，「买过」就不可能成立 ——
//     把它当故障上报会让一次入参问题变成一条错误日志 + 一句归口文案。
//
// projectID 不在这里判形状：空 / 非法工程在本仓是「查不到行」，返回 false 是正确结论
// （同 SpentTotalsByProject 的判据）。
func (s *Service) HasPurchasedProduct(ctx context.Context, projectID string, userID uint64, productID string) (purchased bool, err error) {
	productID = strings.TrimSpace(productID)
	if userID == 0 || productID == "" {
		return false, nil
	}
	if _, perr := uuid.Parse(productID); perr != nil {
		return false, nil
	}
	return s.orders.HasPurchasedProduct(ctx, strings.TrimSpace(projectID), userID, productID)
}

// 编译期断言：装配层用它把本 service 作为 PurchaseSource 注入 membership。
//
// 分一条断言而不是并进 ordercontract.OrderService：这份能力**不是**订单对外的业务契约
// （客户页 / 购物车 / 片段层都不该拿到「全站消费额清单」），它只服务会员重算这一条路径。
// 装配点用断言取，缺实现时在启动时炸掉，而不是等到日结跑出「扫到 0 个人」。
var _ membershipcontract.PurchaseSource = (*Service)(nil)

// SpentTotalsByUser 返回本工程「有可计入消费」的访客 → 累计消费额（**分**）。
//
// 口径（哪些订单状态计入、是否减去退款）由订单侧定义并负责 —— 这里是**唯一的出口**：
// model.SpentTotalsByProject 一条聚合取回，与客户页的订单摘要（SummaryByUser）
// 共用同一份 paidStatuses 与同一个 rls.InProjectScope 形态，两边不可能分叉。
//
// 工程 id 为空即参数错误（打回给调用方），不返回空 map：「没给工程」与「这个工程
// 一分钱消费都没有」在排障时方向完全相反，用一个空 map 表达两件事会让后者被前者掩盖。
func (s *Service) SpentTotalsByUser(ctx context.Context, projectID string) (totals map[uint64]int64, err error) {
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	return s.orders.SpentTotalsByProject(ctx, pid)
}
