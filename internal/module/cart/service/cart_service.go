package cartservice

// cart_service.go — 购物车模块的 Service 装配。
//
// 依赖四条，都经过收窄：
//   · order 的 OrderService（建单与支付落账的唯一入口）
//   · product 的 VariantSnapshotPort（只读商品事实：名称 / 规格 / 价格 / 成本）
//   · product 的 VariantAvailabilityLookupPort（只读可用量，可缺）
//   · cartcontract.PaymentGateway（收窄到「一个通道」：开通、标题、扣款）
//
// 不持有 *gorm.DB，也没有自己的表：购物车状态在客户端 cookie 里（见 cart.go）。
// 「无持久化的模块」是有意的 —— 服务端存购物车要先解决「没登录的访客是谁」，
// 而那正是我们想避开的注册门槛。

import (
	cartcontract "go_wp/internal/module/cart/contract"
	membershipcontract "go_wp/internal/module/membership/contract"
	ordercontract "go_wp/internal/module/order/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// Service 购物车用例。
type Service struct {
	orders  ordercontract.OrderService
	product productcontract.VariantSnapshotPort
	// availability 可用量查询（渲染与加购时校验）。
	//
	// 允许为 nil：未接入时购物车照常可用，只是不显示「仅剩 N 件」、加购不做库存预检 ——
	// 最终的把关在结算写路径上（订单域扣库存时按真源判定，不足即整体拒绝）。
	// 这与 productVariantAvailability 片段的降级口径一致：读不到库存不该让页面报错。
	availability productcontract.VariantAvailabilityLookupPort
	// pay 支付通道。**不允许为 nil**：没有通道的结算只会建出一堆永远付不了款的单。
	pay cartcontract.PaymentGateway
	// membership 会员身份读取端口（BIZ-3 消费侧接入，装配期经 SetMembershipReader 注入）。
	//
	// 允许为 nil：未注入即「会员权益未开启」，结算运费与接入前逐字一致
	//（见 cart.go 里运费判据）。收窄到 Reader —— 购物车只要「免不免运费」这一条。
	membership membershipcontract.Reader
	// shippingPolicy 站点运费规则读取端口（站点级基础运费 + 满额免运费门槛，装配期经
	// SetShippingPolicyReader 注入）。
	//
	// 允许为 nil：未注入即「这个站点没有配置运费」—— 结算运费恒 0，与接入前逐字一致
	//（接入前 base 由调用方给、而前台链路恒 0）。注意这与「会员端口未注入」不是同一件事：
	// 那边的 base 已知（由本端口给），只是不减免；这里连 base 都读不到。
	// 收窄成一条只读方法：结算链路上拿不到工程 CRUD 与站点设置写入（见 project 的 contract）。
	shippingPolicy projectcontract.ShippingPolicyReader

	codec cookieCodec
}

// NewService 构造（参数直传，不用 Deps 结构体）。
//
// secret 为空时 codec 仍可工作（HMAC 用空密钥），但那就等于没有签名 ——
// 装配层必须传真实密钥（与 auth.session_secret 同源），否则整条防篡改边界形同虚设。
func NewService(
	orders ordercontract.OrderService,
	product productcontract.VariantSnapshotPort,
	availability productcontract.VariantAvailabilityLookupPort,
	pay cartcontract.PaymentGateway,
	secret string,
) *Service {
	return &Service{
		orders:       orders,
		product:      product,
		availability: availability,
		pay:          pay,
		codec:        cookieCodec{secret: []byte(secret)},
	}
}

// 编译期断言：本 service 实现模块对外契约。
var _ cartcontract.CartService = (*Service)(nil)
