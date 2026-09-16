package orderservice

// order_service.go — 订单模块的 Service 装配（BIZ-1 销售侧）。
//
// 依赖都经过收窄：
//   · product 的 VariantSnapshotPort（只读变体快照，一个方法）
//   · ordercontract.StockOperator（扣减与增加，两个方法）
//   · webhook 的 Dispatcher（派发事件，一个方法，可选）
//
// 不持有 *gorm.DB：持久化唯一入口是 model 的具名方法；跨表事务由本层编排。

import (
	ordercontract "go_wp/internal/module/order/contract"
	ordermodel "go_wp/internal/module/order/model"
	productcontract "go_wp/internal/module/product/contract"
	usercontract "go_wp/internal/module/user/contract"
	webhookcontract "go_wp/internal/module/webhook/contract"
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
	// 未接线时整体跳过，不报错（见 order_events.go 的 dispatchEvent）。
	webhooks webhookcontract.Dispatcher
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
