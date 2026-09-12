// Package cartcontract 购物车模块对外契约。
package cartcontract

import (
	"context"

	cartdto "go_wp/internal/module/cart/dto"
)

// 购物车 cookie 的名字与寿命。
//
// 放在契约包而不是 service 包：写这个 cookie 的是**访问面的片段处理器**，
// 而跨模块只允许依赖对方的 contract 与不可变 dto —— 片段层不该为了一个 cookie 名
// 去碰购物车模块的 service。
const (
	// CartCookieName 购物车 cookie 名。
	CartCookieName = "gw_cart"
	// CartCookieMaxAgeSeconds 购物车有效期（30 天）：够长到「过几天回来还能接着结」，
	// 又不会让半年后的一次误点复活一辆旧车。
	CartCookieMaxAgeSeconds = 30 * 24 * 3600
)

// CartService 购物车与访客结算能力。
//
// 变更类方法（Add / SetQuantity / Clear）返回的快照里带 Cookie（要写回响应），
// View 是纯读、Cookie 恒为空 —— 「要不要动响应头」由返回值的这一处区分。
type CartService interface {
	// Add 加入购物车：同变体累加数量，超过上限即整体拒绝（不悄悄截断）。
	Add(ctx context.Context, req *cartdto.CartAddReq) (res *cartdto.CartSnapshot, err error)
	// SetQuantity 设置某变体数量：0 表示移除。
	SetQuantity(ctx context.Context, req *cartdto.CartSetQuantityReq) (res *cartdto.CartSnapshot, err error)
	// Clear 清空购物车。
	Clear(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error)
	// View 只读查询（渲染购物车与计数片段）。
	View(ctx context.Context, req *cartdto.CartViewReq) (res *cartdto.CartSnapshot, err error)
	// Checkout 结算：cookie 购物车 → 订单 → 支付 → 落账。
	//
	// 返回 error 只表示「订单没建出来」（商品下架、库存不足、参数缺失）；
	// 订单建成而支付没走通时返回 Paid=false 的成功结果，订单号照样给到访客。
	Checkout(ctx context.Context, req *cartdto.CartCheckoutReq) (res *cartdto.CheckoutResp, err error)
}

// PaymentGateway 支付通道。
//
// 接口定义在**使用方**（本模块的结算流程）而不是订单域：订单域只认识
// 「支付方式 / 支付流水号」两个列，它不需要知道世界上存在网关这个东西。
// 将来接真通道（PayPal / Stripe / 微信支付）时加一个实现、装配时换一行，
// 购物车与订单的代码都不用动。
type PaymentGateway interface {
	// Method 通道标识，落 orders.payment_method（如 paypal）。
	Method() string
	// Title 通道展示名，落 orders.payment_method_title，也是片段里给访客看的名字。
	Title() string
	// Charge 发起扣款。成功返回通道流水号。
	//
	// 实现必须是**幂等**的：同一个 OrderNo 重复调用返回同一个流水号，
	// 绝不能扣两次钱（结算流程在重试与重发下会重复调用它）。
	Charge(ctx context.Context, req *PaymentChargeReq) (res *PaymentChargeResult, err error)
}

// PaymentChargeReq 扣款请求。
type PaymentChargeReq struct {
	OrderNo  string
	Amount   int64 // 分
	Currency string
	Email    string
	// ReturnURL 支付完成后用户应回到的地址（真通道要跳转；模拟通道忽略）。
	ReturnURL string
}

// PaymentChargeResult 扣款结果。
type PaymentChargeResult struct {
	TransactionID string
	// Sandbox 标记这是模拟通道：后台一眼能分出「演示单」与「真金白银的单」，
	// 免得把测试订单当成真营收去对账。
	Sandbox bool
}
