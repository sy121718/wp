// Package cartenums 购物车模块的响应与错误消息。
//
// 与 order / user 模块同口径：文案直接是中文常量（未接 i18n），展示层拿
// UserFacingMessages 做白名单，不在表里的一律收口到 ErrInternal。
package cartenums

const (
	MsgCartUpdated = "购物车已更新"
	MsgCartCleared = "购物车已清空"
)

// 参数与校验。
const (
	ErrInvalidParam    = "参数不合法"
	ErrProjectRequired = "缺少站点工程"
	ErrVariantRequired = "缺少商品规格"
	ErrQuantityInvalid = "商品数量必须为正整数"
	ErrQuantityTooMany = "单件商品的数量超出上限"
	ErrCartEmpty       = "购物车是空的"
	ErrCartFull        = "购物车里放不下更多商品了，请先结算或清空"
	ErrCartItemAbsent  = "购物车里没有这件商品"
)

// 商品与库存。
const (
	ErrVariantNotFound = "商品规格不存在或已下架"
	ErrOutOfStock      = "库存不足，无法下单"
)

// 结算信息。
const (
	ErrEmailRequired   = "请填写邮箱，订单与账号信息会发到这里"
	ErrEmailInvalid    = "邮箱格式不正确"
	ErrNameRequired    = "请填写收货人姓名"
	ErrPhoneRequired   = "请填写联系电话"
	ErrAddressRequired = "请填写收货地址"
)

// 支付。
const (
	// ErrPaymentFailed 支付通道没扣成：**订单已经建好了**，留在待付款，
	// 访客可以稍后重试或换成人工处理，绝不能因为扣款失败就把订单丢掉。
	ErrPaymentFailed = "支付未成功，订单已创建，请稍后在订单中继续支付"
	// ErrCallbackSignature 回调验签失败。对外只说「签名不合法」，
	// 不区分「没有签名 / 签名错误 / 用了旧密钥」—— 那是给攻击者的信息。
	ErrCallbackSignature = "回调签名校验失败"
	// ErrCallbackOrderMissing 回调里的商户单号在本站找不到。
	ErrCallbackOrderMissing = "回调对应的订单不存在"
	// ErrCallbackAmountMismatch 回调金额与订单总额不一致：
	// 入账会让账目对不平，所以宁可停在这一步让人来看。
	ErrCallbackAmountMismatch = "回调金额与订单金额不一致"
)

// ErrInternal 未归类的系统错误对外统一文案。
const ErrInternal = "操作失败，请稍后重试"

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
