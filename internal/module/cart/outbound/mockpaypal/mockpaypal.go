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
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	cartcontract "go_wp/internal/module/cart/contract"
)

// 通道标识与展示名（落 orders.payment_method / payment_method_title）。
const (
	methodCode = "paypal"
	// methodName 通道展示名；methodNameKey 是它的词条 key（key + 中文兜底成对）。
	//
	// 展示名会被**写进 orders.payment_method_title** —— 那是「这单当时是怎么付的」快照，
	// 历史单据的语言不该随当前界面语言变化，所以落库仍用中文兜底。
	// key 备好供「通道名按界面语言展示」的批次使用（展示层 tr(keyName, name)）。
	methodName    = "PayPal（模拟）"
	methodNameKey = "cart.payment.paypalMock"
	// idPrefix 让流水号自带「这是模拟单」的标记。
	idPrefix = "MOCKPAYPAL-"
)

// Gateway 模拟通道。
//
// secret 只服务**回调验签**：Charge 是同步调用（调用方就是我们自己的结算流程），
// 不需要签名；而回调是**从外部打进来的**，必须有办法判断这条通知真是通道发的。
// 没有密钥时拒绝一切回调 —— 而不是退化成「信任所有回调」。
type Gateway struct{ secret []byte }

// New 构造。secret 为空时通道仍能扣款，但回调一律拒绝（见 VerifyCallback）。
func New(secret string) *Gateway { return &Gateway{secret: []byte(secret)} }

// Method 通道标识。
func (g *Gateway) Method() string { return methodCode }

// Title 通道展示名（落库快照值，见 methodName 的注释）。
func (g *Gateway) Title() string { return methodName }

// TitleKey 通道展示名的词条 key（与 Title 成对：key + 中文兜底）。
func (g *Gateway) TitleKey() string { return methodNameKey }

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

// callbackSignatureHeader 模拟通道的回调签名头（键为小写，与 handler 的归一化口径一致）。
const callbackSignatureHeader = "x-mock-signature"

// Sign 计算回调签名：hex(HMAC-SHA256(secret, rawBody))。
//
// 导出是为了让**测试与本地联调**能造出合法签名 —— 验签算法只有一份实现，
// 测试里再手写一遍就等于把「算法是什么」写了两处，改一处漏一处。
func Sign(secret string, rawBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCallback 校验回调签名并解出支付事实。
//
// 顺序不能反：**先验签、后解析**。先解析再判断，等于让未认证的字节先进到
// 业务类型的构造路径里；而一旦有人图省事把「解析失败」当成「格式不对，跳过验签」，
// 伪造回调就只剩字段拼装。
func (g *Gateway) VerifyCallback(headers map[string]string, rawBody []byte) (res *cartcontract.PaymentCallback, err error) {
	if len(g.secret) == 0 {
		return nil, errors.New("支付通道未配置回调密钥")
	}
	got := strings.TrimSpace(headers[callbackSignatureHeader])
	if got == "" {
		return nil, errors.New("回调缺少签名")
	}
	want := Sign(string(g.secret), rawBody)
	// 常量时间比较：普通的字符串比较会在第一个不同的字节处提前返回，
	// 理论上可以被用来逐字节试探签名。
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(got)), []byte(want)) != 1 {
		return nil, errors.New("回调签名不匹配")
	}
	var payload struct {
		OrderNo       string `json:"orderNo"`
		TransactionID string `json:"transactionId"`
		Status        string `json:"status"`
		Amount        int64  `json:"amount"`
		Currency      string `json:"currency"`
	}
	if uerr := json.Unmarshal(rawBody, &payload); uerr != nil {
		return nil, errors.New("回调报文解析失败")
	}
	orderNo := strings.TrimSpace(payload.OrderNo)
	if orderNo == "" {
		return nil, errors.New("回调缺少订单号")
	}
	return &cartcontract.PaymentCallback{
		OrderNo:       orderNo,
		TransactionID: strings.TrimSpace(payload.TransactionID),
		// 只有明确的成功状态才算付款：其余（failed / canceled / 未知）一律按未成功处理，
		// 把未知当成功是这一层最危险的默认值。
		Paid:        strings.EqualFold(strings.TrimSpace(payload.Status), "success") || strings.EqualFold(strings.TrimSpace(payload.Status), "completed"),
		Amount:      payload.Amount,
		Method:      methodCode,
		MethodTitle: methodName,
		Sandbox:     true,
	}, nil
}

// 编译期断言：实现购物车模块的支付通道契约。
var _ cartcontract.PaymentGateway = (*Gateway)(nil)
