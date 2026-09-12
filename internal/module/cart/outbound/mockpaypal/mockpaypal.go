// Package mockpaypal 模拟 PayPal 支付通道。
//
// 为什么要有它：结算链路（建单 → 扣款 → 落账）必须现在就跑通，而接真通道要处理
// 商户资质、回调公网可达、签名验签、沙箱账号 —— 那些是**通道的问题**，不是订单与购物车
// 的问题。用一个符合契约的假通道把链路先跑通，将来换成真实现只需改装配那一行。
//
// 它同时也是契约那几条要求的可执行说明：
//
//	· Method / Title 告诉订单表「这单是怎么付的」；
//	· Charge 的幂等要求在这里用「流水号由订单号派生」实现 —— 同一个订单号重复调用
//	  必然得到同一个流水号，**不需要任何共享状态**，因此多实例部署下同样成立。
//	  真通道用另一种方式做到同一件事（PayPal 认 PayPal-Request-Id 头），
//	  但契约对调用方的保证是一致的。
//
// 落库的 payment_method 是 paypal、payment_method_title 是「PayPal（模拟）」：
// 后台一眼能分出演示单与真金白银的单，不会把测试订单当成真营收去对账。
package mockpaypal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	cartcontract "go_wp/internal/module/cart/contract"
)

// 通道标识与展示名（落 orders.payment_method / payment_method_title）。
const (
	methodCode = "paypal"
	methodName = "PayPal（模拟）"
	// idPrefix 让流水号自带「这是模拟单」的标记。
	idPrefix = "MOCKPAYPAL-"
)

// Gateway 模拟通道（无状态，可零值使用）。
type Gateway struct{}

// New 构造。
func New() *Gateway { return &Gateway{} }

// Method 通道标识。
func (g *Gateway) Method() string { return methodCode }

// Title 通道展示名。
func (g *Gateway) Title() string { return methodName }

// Charge 模拟扣款：校验入参后按订单号派生一个稳定的流水号。
//
// 不引入「随机号 + 台账」：那需要一份共享状态（多实例下就得进 Redis 或数据库），
// 而派生式流水号既满足幂等，又让「同一个订单永远是同一个号」这件事看得见。
func (g *Gateway) Charge(_ context.Context, req *cartcontract.PaymentChargeReq) (res *cartcontract.PaymentChargeResult, err error) {
	if req == nil {
		return nil, errors.New("支付请求为空")
	}
	orderNo := strings.TrimSpace(req.OrderNo)
	if orderNo == "" {
		// 没有商户单号的扣款无法对账，也无法幂等 —— 直接拒绝，不留一个「无主」的支付。
		return nil, errors.New("缺少订单号")
	}
	if req.Amount <= 0 {
		return nil, errors.New("支付金额必须为正")
	}
	sum := sha256.Sum256([]byte(methodCode + ":" + orderNo))
	return &cartcontract.PaymentChargeResult{
		TransactionID: idPrefix + strings.ToUpper(hex.EncodeToString(sum[:8])),
		Sandbox:       true,
	}, nil
}

// 编译期断言：实现购物车模块的支付通道契约。
var _ cartcontract.PaymentGateway = (*Gateway)(nil)
