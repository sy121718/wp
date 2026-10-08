package orderservice

//
// 依赖都经过收窄：
//   · product 的 VariantSnapshotPort（只读变体快照，一个方法）
//   · ordercontract.StockOperator（扣减与增加，两个方法）
//   · webhook 的 Dispatcher（派发事件，一个方法，可选）
//
// 不持有 *gorm.DB：持久化唯一入口是 model 的具名方法；跨表事务由本层编排。

//
// orders / order_returns / coupons 在迁移 215 里都带 FORCE 策略：不带 app.project_id 的
// 读写在非超级角色下**静默落空**（读到 nil、更新 0 行、锁不住行），而这些入口的请求里
// 只有 id —— 后台订单管理页（取消 / 改状态 / 退款 / 备注）、支付回调、访客撤销退货申请、
// 超时取消扫描都属此类。
//
// 做法与 page 侧同形：**先逐工程独立作用域探测出归属**（id 是主键，跨工程不会重复命中），
// 拿到实体自带的 project_id 之后再进事务 —— 事务内的作用域用它，绝不退回「不限工程」。
// 方向也是安全的：探测本身受策略约束，拿不到别的工程的行。
//
// 为什么探测放在事务**外**：rls.InProjectScope 会新开事务、另取连接，放进已开的事务里
// 会让外层未提交的数据不可见、同表写入还可能自锁（见 pkg/rls.ScopeTx 的说明）。
// 工程归属是稳定属性（订单不会换工程），所以事务外取到的作用域在事务内依然成立。

//
// 这里是「业务事实 → 外部通知」的唯一转换点。三条设计约束：
//
//  1. 通知**不是事务的一部分**：一律在业务事务提交之后派发，失败只记日志 ——
//     对端收不到通知不该让已经成立的事实回滚（钱已经收了，不能因为 webhook 挂了退回去）。
//     跨模块事务不存在，也不该为一次通知造一个。
//  2. 载荷只放**不可变事实**：订单号、金额、币种、支付方式、交易号。
//     刻意不含客户邮箱 / 手机号 / 收货地址 —— 端点是我们自己登记的，但它拿到的东西
//     不该多于完成这件事所需（数据最小化；投递日志还会把载荷原文留在库里）。
//  3. 派发口是**可选依赖**：未接线时整体跳过。webhook 是插件生态的外部能力，
//     不是订单域的必需品，缺它不该让订单模块起不来。

import (
	"context"
	"errors"
	"strings"
	"time"

	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"
	"go_wp/pkg/logger"
)

// Service 订单模块用例。
type Service struct {
	orders  *ordermodel.OrderModel
	items   *ordermodel.OrderItemModel
	logs    *ordermodel.OrderStatusLogModel
	coupons *ordermodel.CouponModel
	returns *ordermodel.ReturnModel
	product productcontract.VariantSnapshotPort
	stock   ordercontract.StockOperator
	// guest 访客开号：下单邮箱没有账号时建一个并回填 user_id。
	// 允许为 nil（装配期未接用户模块时，下单仍可用，只是不自动开号）。
	guest usercontract.GuestAccountProvisioner
	// webhooks 外部集成派发口（OSS-006）：订单事件向管理员登记的端点各排一次投递。
	// 允许为 nil —— webhook 是插件生态的外部能力，不是订单域的必需品；
	// 未接线时整体跳过，不报错（见 dispatchEvent）。
	webhooks webhookcontract.Dispatcher
	// projects 工程清单来源（DB-009 第四批）：orders/epc 带 FORCE 策略，一批只带 id 的
	// 入口（后台订单操作、支付回调、超时扫描）需要先逐工程探测出归属。
	// 必填：装配点尚未注入时逐工程定位直接失败（不再回退到直读 projects 表）
	//（那条路径的落点与消除办法见 projectIDs 与 DB-009 报告）。
	projects projectcontract.ProjectService
	// membership 会员身份读取端口（BIZ-3 消费侧接入，装配期经 SetMembershipReader 注入）。
	//
	// 允许为 nil：未注入即「会员折扣未开启」，建单金额与本端口接入前逐字一致
	//（见 resolveMembershipDiscount）。这里刻意是**收窄的 Reader** 而不是
	// MembershipService —— 订单只需要「读一条会员身份」，拿不到等级 CRUD 与归属写入能力。
	membership membershipcontract.Reader
}

// NewService 构造（参数直传，不用 Deps 结构体）。
//
// product / stock 允许为 nil 吗：**不允许**。建单要落商品快照、要扣库存，
// 任一缺失都只能在建单那一刻失败；装配期 fail-fast 比运行时逐个请求失败好得多。
func NewService(
	orders *ordermodel.OrderModel,
	items *ordermodel.OrderItemModel,
	logs *ordermodel.OrderStatusLogModel,
	coupons *ordermodel.CouponModel,
	returns *ordermodel.ReturnModel,
	product productcontract.VariantSnapshotPort,
	stock ordercontract.StockOperator,
	guest usercontract.GuestAccountProvisioner,
	webhooks webhookcontract.Dispatcher,
) *Service {
	return &Service{
		orders: orders, items: items, logs: logs, coupons: coupons, returns: returns,
		product: product, stock: stock, guest: guest,
		webhooks: webhooks,
	}
}

// 编译期断言：本 service 实现模块对外契约。
var _ ordercontract.OrderService = (*Service)(nil)

// SetProjects 注入工程契约（装配期调用）。
//
// 注入口径：契约只用来「列出工程 id」，而这个清单的所有权在 project 模块 —— 本模块的
// model 层不读 projects 表。未注入时 projectIDs 直接失败（不再回退到直接读表）。
func (s *Service) SetProjects(projects projectcontract.ProjectService) {
	if s == nil {
		return
	}
	s.projects = projects
}

// projectIDs 定位与扇出用的工程清单。
//
// 工程表为空时显式失败：静默返回空清单会把「读不到工程表」伪装成「没有超时订单 /
// 订单不存在」，那正是本批要消灭的 fail-silent。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.orders == nil || s.projects == nil {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	return ids, nil
}

// locateOrderProject 逐工程探测订单归属工程；找不到返回 ErrOrderNotFound。
//
// GetByID(ctx, id, projectID) 在不存在时返回 (nil, nil)，所以「命中」的判据是实体非 nil。
func (s *Service) locateOrderProject(ctx context.Context, orderID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.orders.GetByID(ctx, orderID, pid)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", errors.New(orderenums.ErrOrderNotFound)
}

// resolveOrderProject 优先用调用方显式传入的工程；否则逐工程探测（DB-009 第四批）。
//
// 显式优先的意义：已经知道工程的调用方（超时取消扫描逐工程调用）不必再多一轮探测。
func (s *Service) resolveOrderProject(ctx context.Context, orderID uint64, explicit string) (string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return pid, nil
	}
	return s.locateOrderProject(ctx, orderID)
}

// locateReturnProject 逐工程探测退货单归属工程；找不到返回 ErrReturnNotFound。
func (s *Service) locateReturnProject(ctx context.Context, returnID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.returns.GetByID(ctx, pid, returnID)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", errors.New(orderenums.ErrReturnNotFound)
}

// locateCouponProject 逐工程探测优惠码归属工程；找不到返回 ErrCouponNotFound。
func (s *Service) locateCouponProject(ctx context.Context, couponID uint64) (string, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return "", err
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.coupons.GetByID(ctx, pid, couponID)
		if gerr != nil {
			return "", gerr
		}
		if e != nil {
			return e.ProjectID, nil
		}
	}
	return "", errors.New(orderenums.ErrCouponNotFound)
}

// OrderPaidEvent 订单支付成功事件的载荷（webhook 事件名 order.paid）。
//
// 金额一律是**最小币种单位的整数**（与 orders.total 同口径，人民币即「分」）：
// 浮点金额在跨语言的 JSON 通道上会出现 0.1+0.2 这类误差，接收方拿到 19.99
// 还是 19.989999 取决于它用哪个解析器 —— 整数分是精确的，换算责任交给展示层。
type OrderPaidEvent struct {
	// OrderID 订单主键（内部流水 id，接收方一般用 OrderNo 做业务引用）。
	OrderID uint64 `json:"orderId"`
	// OrderNo 订单号（对外业务引用）。
	OrderNo string `json:"orderNo"`
	// Status 支付后的订单状态（恒为 paid）。
	Status string `json:"status"`
	// Currency 币种代码。
	Currency string `json:"currency"`
	// Total 应付总额（最小币种单位）。
	Total int64 `json:"total"`
	// Subtotal / DiscountTotal / ShippingTotal / TaxTotal 金额分解（同口径）。
	Subtotal      int64 `json:"subtotal"`
	DiscountTotal int64 `json:"discountTotal"`
	ShippingTotal int64 `json:"shippingTotal"`
	TaxTotal      int64 `json:"taxTotal"`
	// PaymentMethod 支付方式标识（网关代号或手工登记的方式）。
	PaymentMethod string `json:"paymentMethod"`
	// TransactionID 通道流水号（手工登记 / 模拟通道可能为空）。
	TransactionID string `json:"transactionId"`
	// PaidAt 支付落账时刻。
	PaidAt time.Time `json:"paidAt"`
}

// dispatchEvent best-effort 派发一次对外事件：未接线即跳过，失败只记日志。
//
// 返回的入队条数刻意不透出：调用方（支付落账）拿它做不了任何决定，
// 透出只会诱使上层写「派发给几个端点」这类依赖外部配置的分支。
func (s *Service) dispatchEvent(ctx context.Context, eventType string, payload any) {
	if s.webhooks == nil {
		return // 未接线：外部通道未开启，不是错误，也不是降级。
	}
	if _, err := s.webhooks.DispatchEvent(ctx, eventType, payload); err != nil {
		// 只记日志：通知失败不能影响已经落账的支付。
		logger.Error(err, "订单事件派发失败（不影响业务事实）："+eventType)
	}
}
