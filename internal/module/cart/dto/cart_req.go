package cartdto

// cart_req.go — 购物车模块请求。
//
// 购物车的状态在**客户端 cookie** 里（访客未登录也要能加购，所以不能依赖会话）。
// 因此本模块的每个请求都带一个 Cookie 字段：那是请求里购物车 cookie 的原始值，
// 由 inbound 取出来交给本模块，由本模块负责解码、验签、重新签名后交回 inbound 写回响应。
// 这样「cookie 的格式与真伪」只有一处实现，HTTP 层只做搬运。

import ordercontract "go_wp/internal/module/order/contract"

// TrackCookies 访客追踪 cookie 的原始值（流量来源 / 首触 / 会话 / 浏览轨迹）。
//
// 与购物车 cookie 分开传：购物车是「要买什么」，追踪是「从哪来的」，
// 前者是业务状态、后者是可选的增强数据 —— 追踪 cookie 全空时下单照常，
// 只是订单归因落成空对象。
type TrackCookies struct {
	Current string
	First   string
	Session string
	Trail   string
	// Visitor 访客计数 cookie（跨会话持久：第几次来、第一次到站的时间）。
	// 会话 cookie 一关浏览器就没了，数不清这件事。
	Visitor string
}

// CartAddReq 加入购物车（同变体累加数量）。
type CartAddReq struct {
	ProjectID string
	VariantID string
	Quantity  int
	Cookie    string
}

// CartSetQuantityReq 直接设置某变体的数量（0 = 从购物车移除）。
//
// 用「设置」而不是「加减」：加号按钮连点两次与网络重发会各发一个请求，
// 加减语义下这就是两件商品，而设置语义下结果恒定 —— 幂等在这里是免费的。
type CartSetQuantityReq struct {
	ProjectID string
	VariantID string
	Quantity  int
	Cookie    string
}

// CartViewReq 只读查询购物车。
type CartViewReq struct {
	ProjectID string
	Cookie    string
}

// PaymentCallbackReq 支付通道的异步回调。
//
// ProjectID 从**回调 URL 的查询参数**取（通道只回传它拿到的那些东西，
// 工程 id 是我们自己拼进 notify_url 的）；Headers 是原始请求头（键已归一化为小写），
// RawBody 是**未经解析的原始报文** —— 验签要拿它算，不能拿解析结果算。
type PaymentCallbackReq struct {
	ProjectID string
	Headers   map[string]string
	RawBody   []byte
}

// CartCheckoutReq 结算：把 cookie 里的购物车变成一张订单并完成支付。
type CartCheckoutReq struct {
	ProjectID string
	Cookie    string

	// 联系与收货信息。邮箱是必填的：订单归属、访客自动开号的账号、初始密码
	// 都发到这个地址上，缺了它就等于下了一单无处可查的订单。
	Email string
	Name  string
	Phone string
	// Shipping 收货地址（复用订单域的不可变 DTO，避免两处字段名悄悄分叉）。
	Shipping ordercontract.OrderAddress
	Billing  ordercontract.OrderAddress
	// Remark 客户备注（与后台备注 adminNote 分开）。
	Remark string
	// RequestID 幂等键：结算页渲染时生成，重复提交只落一单。
	RequestID string
	// Locale 访客语言（决定自动开号的初始密码邮件用哪套模板）。
	Locale string

	Tracking TrackCookies

	// 以下由 inbound 覆盖写入，客户端不可伪造。
	UserID    *uint64
	IPAddress string
	UserAgent string
}
