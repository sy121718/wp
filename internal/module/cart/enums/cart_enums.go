// Package cartenums 购物车模块的响应与错误消息。
//
// 与 order / user 模块同口径：**常量的值是 i18n key**（审计 I18N-002），真正的文案在

// sys_i18n（迁移 179 seed）。响应层 pkg/response.translate 按请求语言查表，未命中时

// 原样返回 key —— 缺资源的部署看到的是 key 而不是空白，问题可见。展示层仍拿

// UserFacingMessages 做白名单（白名单列的是常量名，key 化后自动同步）。
// UserFacingMessages 做白名单，不在表里的一律收口到 ErrInternal。
package cartenums

import "go_wp/pkg/facing"

const (
	MsgCartUpdated = "cart.msg.updated"
	MsgCartCleared = "cart.msg.cleared"
)

// 支付回调的响应消息（回给**通道**，不是访客页文案）。
//
// 与其它消息同口径：值是 i18n key，响应层按请求语言查表；未命中时原样返回 key，
// 问题可见。原先这三句是 handler / service 里硬编码的中文。
const (
	MsgCallbackReceived    = "cart.msg.callbackReceived"
	MsgCallbackApplied     = "cart.msg.callbackApplied"
	MsgCallbackUnpaid      = "cart.msg.callbackUnpaid"
	MsgCallbackNeedsReview = "cart.msg.callbackNeedsReview"
	MsgCallbackAlreadyPaid = "cart.msg.callbackAlreadyPaid"
)

// 参数与校验。
const (
	ErrInvalidParam    = "cart.err.invalidParam"
	ErrProjectRequired = "cart.err.projectRequired"
	ErrVariantRequired = "cart.err.variantRequired"
	ErrQuantityInvalid = "cart.err.quantityInvalid"
	ErrQuantityTooMany = "cart.err.quantityTooMany"
	ErrCartEmpty       = "cart.err.cartEmpty"
	ErrCartFull        = "cart.err.cartFull"
	ErrCartItemAbsent  = "cart.err.cartItemAbsent"
)

// 商品与库存。
const (
	ErrVariantNotFound = "cart.err.variantNotFound"
	ErrOutOfStock      = "cart.err.outOfStock"
)

// 结算信息。
const (
	ErrEmailRequired   = "cart.err.emailRequired"
	ErrEmailInvalid    = "cart.err.emailInvalid"
	ErrNameRequired    = "cart.err.nameRequired"
	ErrPhoneRequired   = "cart.err.phoneRequired"
	ErrAddressRequired = "cart.err.addressRequired"
)

// 支付。
const (
	// ErrPaymentFailed 支付通道没扣成：**订单已经建好了**，留在待付款，
	// 访客可以稍后重试或换成人工处理，绝不能因为扣款失败就把订单丢掉。
	ErrPaymentFailed = "cart.err.paymentFailed"
	// ErrCallbackSignature 回调验签失败。对外只说「签名不合法」，
	// 不区分「没有签名 / 签名错误 / 用了旧密钥」—— 那是给攻击者的信息。
	ErrCallbackSignature = "cart.err.callbackSignature"
	// ErrCallbackOrderMissing 回调里的商户单号在本站找不到。
	ErrCallbackOrderMissing = "cart.err.callbackOrderMissing"
	// ErrCallbackAmountMismatch 回调金额与订单总额不一致：
	// 入账会让账目对不平，所以宁可停在这一步让人来看。
	ErrCallbackAmountMismatch = "cart.err.callbackAmountMismatch"
)

// ErrInternal 未归类的系统错误对外统一文案。
const ErrInternal = "cart.err.internal"

// UserFacingMessages 可以原样展示给访客的全部业务文案（白名单）。
var UserFacingMessages = []string{
	MsgCartUpdated, MsgCartCleared,
	ErrInvalidParam, ErrProjectRequired, ErrVariantRequired,
	ErrQuantityInvalid, ErrQuantityTooMany, ErrCartEmpty, ErrCartFull, ErrCartItemAbsent,
	ErrVariantNotFound, ErrOutOfStock,
	ErrEmailRequired, ErrEmailInvalid, ErrNameRequired, ErrPhoneRequired, ErrAddressRequired,
	ErrPaymentFailed,
	ErrInternal,
}

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `cart.err.invalidParam` 这样的裸 key。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrInvalidParam:           "参数不合法",
	ErrProjectRequired:        "缺少站点工程",
	ErrVariantRequired:        "缺少商品规格",
	ErrQuantityInvalid:        "商品数量必须为正整数",
	ErrQuantityTooMany:        "单件商品的数量超出上限",
	ErrCartEmpty:              "购物车是空的",
	ErrCartFull:               "购物车里放不下更多商品了，请先结算或清空",
	ErrCartItemAbsent:         "购物车里没有这件商品",
	ErrVariantNotFound:        "商品规格不存在或已下架",
	ErrOutOfStock:             "库存不足，无法下单",
	ErrEmailRequired:          "请填写邮箱，订单与账号信息会发到这里",
	ErrEmailInvalid:           "邮箱格式不正确",
	ErrNameRequired:           "请填写收货人姓名",
	ErrPhoneRequired:          "请填写联系电话",
	ErrAddressRequired:        "请填写收货地址",
	ErrPaymentFailed:          "支付未成功，订单已创建，请稍后在订单中继续支付",
	ErrCallbackSignature:      "回调签名校验失败",
	ErrCallbackOrderMissing:   "回调对应的订单不存在",
	ErrCallbackAmountMismatch: "回调金额与订单金额不一致",
	ErrInternal:               "操作失败，请稍后重试",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
