package cartenums

import (
	"net/http"

	"go_wp/pkg/facing"
)

const (
	MsgCartUpdated = "cart.msg.updated"
	MsgCartCleared = "cart.msg.cleared"
)

const (
	MsgCallbackReceived    = "cart.msg.callbackReceived"
	MsgCallbackApplied     = "cart.msg.callbackApplied"
	MsgCallbackUnpaid      = "cart.msg.callbackUnpaid"
	MsgCallbackNeedsReview = "cart.msg.callbackNeedsReview"
	MsgCallbackAlreadyPaid = "cart.msg.callbackAlreadyPaid"
)

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

const (
	ErrVariantNotFound = "cart.err.variantNotFound"
	ErrOutOfStock      = "cart.err.outOfStock"
)

const (
	ErrEmailRequired   = "cart.err.emailRequired"
	ErrEmailInvalid    = "cart.err.emailInvalid"
	ErrNameRequired    = "cart.err.nameRequired"
	ErrPhoneRequired   = "cart.err.phoneRequired"
	ErrAddressRequired = "cart.err.addressRequired"
)

const (
	ErrPaymentFailed = "cart.err.paymentFailed"

	ErrCallbackSignature = "cart.err.callbackSignature"

	ErrCallbackOrderMissing = "cart.err.callbackOrderMissing"

	ErrCallbackAmountMismatch = "cart.err.callbackAmountMismatch"
)

const ErrInternal = "cart.err.internal"

var UserFacingMessages = []string{
	MsgCartUpdated, MsgCartCleared,
	ErrInvalidParam, ErrProjectRequired, ErrVariantRequired,
	ErrQuantityInvalid, ErrQuantityTooMany, ErrCartEmpty, ErrCartFull, ErrCartItemAbsent,
	ErrVariantNotFound, ErrOutOfStock,
	ErrEmailRequired, ErrEmailInvalid, ErrNameRequired, ErrPhoneRequired, ErrAddressRequired,
	ErrPaymentFailed,
	ErrInternal,
}

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

func Err(key string) error {
	status := http.StatusBadRequest
	switch key {
	case ErrInternal:
		status = http.StatusInternalServerError
	case ErrVariantNotFound, ErrCartItemAbsent, ErrCallbackOrderMissing:
		status = http.StatusNotFound
	}
	return facing.WithStatus(facing.New(key, errFallbacks[key]), status)
}

func FallbackFor(key string) string { return errFallbacks[key] }
