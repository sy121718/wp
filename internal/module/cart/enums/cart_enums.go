// Package cartenums 购物车模块的响应与错误消息。
//
// 与 order / user 模块同口径：**常量的值是 i18n key**（审计 I18N-002），真正的文案在

// sys_i18n（迁移 179 seed）。响应层 pkg/response.translate 按请求语言查表，未命中时

// 原样返回 key —— 缺资源的部署看到的是 key 而不是空白，问题可见。展示层仍拿

// UserFacingMessages 做白名单（白名单列的是常量名，key 化后自动同步）。
// UserFacingMessages 做白名单，不在表里的一律收口到 ErrInternal。
package cartenums

const (
	MsgCartUpdated = "cart.msg.updated"
	MsgCartCleared = "cart.msg.cleared"
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
