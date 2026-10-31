package orderenums

import (
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	MsgCreateSuccess = "order.msg.createSuccess"
	MsgStatusChanged = "order.msg.statusChanged"
	MsgCancelled     = "order.msg.cancelled"

	MsgRefunded    = "order.msg.refunded"
	MsgPaid        = "order.msg.paid"
	MsgNoteUpdated = "order.msg.noteUpdated"
)

const (
	ErrInvalidParam          = "order.err.invalidParam"
	ErrProjectRequired       = "order.err.projectRequired"
	ErrItemsRequired         = "order.err.itemsRequired"
	ErrItemLimitExceeded     = "order.err.itemLimitExceeded"
	ErrQuantityInvalid       = "order.err.quantityInvalid"
	ErrCustomerEmailRequired = "order.err.customerEmailRequired"
	ErrCustomerEmailInvalid  = "order.err.customerEmailInvalid"
	ErrOrderNoInvalid        = "order.err.orderNoInvalid"
	ErrStatusInvalid         = "order.err.statusInvalid"

	ErrCurrencyUnknown = "order.err.currencyUnknown"

	ErrNoteTooLong = "order.err.noteTooLong"
)

const (
	ErrOrderNotFound = "order.err.orderNotFound"
	ErrOrderNoTaken  = "order.err.orderNoTaken"

	ErrOrderHasNoItems = "order.err.orderHasNoItems"
)

const (
	ErrStatusTransition     = "order.err.statusTransition"
	ErrOrderNotCancellable  = "order.err.orderNotCancellable"
	ErrOrderNotRefundable   = "order.err.orderNotRefundable"
	ErrAlreadyCancelled     = "order.err.alreadyCancelled"
	ErrAlreadyRefunded      = "order.err.alreadyRefunded"
	ErrCancelReasonRequired = "order.err.cancelReasonRequired"
)

const (
	ErrPaymentMethodRequired = "order.err.paymentMethodRequired"

	ErrPaymentChannelFailed = "order.err.paymentChannelFailed"
)

const (
	MsgReturnRequested = "order.msg.returnRequested"
	MsgReturnApproved  = "order.msg.returnApproved"
	MsgReturnRejected  = "order.msg.returnRejected"
	MsgReturnReceived  = "order.msg.returnReceived"
	MsgReturnCancelled = "order.msg.returnCancelled"

	ErrReturnNotFound             = "order.err.returnNotFound"
	ErrReturnItemsRequired        = "order.err.returnItemsRequired"
	ErrReturnQuantityInvalid      = "order.err.returnQuantityInvalid"
	ErrReturnQuantityExceeded     = "order.err.returnQuantityExceeded"
	ErrReturnReasonRequired       = "order.err.returnReasonRequired"
	ErrReturnNotCancellable       = "order.err.returnNotCancellable"
	ErrReturnOrderNotReturnable   = "order.err.returnOrderNotReturnable"
	ErrReturnNotReviewable        = "order.err.returnNotReviewable"
	ErrReturnNotReceivable        = "order.err.returnNotReceivable"
	ErrReturnRejectReasonRequired = "order.err.returnRejectReasonRequired"
	ErrReturnRequestConflict      = "order.err.returnRequestConflict"
)

const (
	ErrVariantNotFound   = "order.err.variantNotFound"
	ErrStockInsufficient = "order.err.stockInsufficient"
	ErrStockUnavailable  = "order.err.stockUnavailable"
)

const (
	ErrOrderNotShippable      = "order.err.orderNotShippable"
	ErrShipQuantityExceeded   = "order.err.shipQuantityExceeded"
	ErrShipLinesRequired      = "order.err.shipLinesRequired"
	ErrShipRequestKeyConflict = "order.err.shipRequestKeyConflict"
	ErrShipWarehouseRequired  = "order.err.shipWarehouseRequired"

	ErrCarrierNotFound  = "order.err.carrierNotFound"
	ErrCarrierCodeTaken = "order.err.carrierCodeTaken"

	ErrCarrierTemplateInvalid = "order.err.carrierTemplateInvalid"
	ErrCarrierStatusInvalid   = "order.err.carrierStatusInvalid"
)

const ErrInternal = "操作失败，请稍后重试"

var errFallbacks = map[string]string{
	ErrBulkRejected: "整批未执行：有条目不能处理，请按明细修正后重试",

	ErrCarrierCodeTaken:           "该承运商编码已被占用",
	ErrCarrierNotFound:            "承运商不存在",
	ErrCarrierStatusInvalid:       "承运商状态不合法",
	ErrCarrierTemplateInvalid:     "承运商模板配置不合法",
	ErrShipWarehouseRequired:      "发货必须指定发货仓",
	ErrShipLinesRequired:          "发货单至少需要一行商品",
	ErrShipQuantityExceeded:       "发货数量超出可发数量",
	ErrReturnRequestConflict:      "这个退货请求已用于另一份申请，请重新发起",
	ErrShipRequestKeyConflict:     "该发货请求已处理过（请求键冲突）",
	ErrOrderNotShippable:          "当前订单状态不允许发货",
	ErrInternal:                   "操作失败，请稍后重试",
	ErrInvalidParam:               "参数不合法",
	ErrProjectRequired:            "缺少站点工程",
	ErrItemsRequired:              "订单至少需要一个商品项",
	ErrItemLimitExceeded:          "单笔订单的商品项超出上限",
	ErrQuantityInvalid:            "商品数量必须为正整数",
	ErrCustomerEmailRequired:      "请填写客户邮箱",
	ErrCustomerEmailInvalid:       "客户邮箱格式不正确",
	ErrOrderNoInvalid:             "订单号格式不合法",
	ErrStatusInvalid:              "订单状态取值不合法",
	ErrCurrencyUnknown:            "该币种不在系统货币字典里（可能已被停用），请重新选择",
	ErrNoteTooLong:                "备注内容过长",
	ErrOrderNotFound:              "订单不存在",
	ErrOrderNoTaken:               "订单号已被占用",
	ErrOrderHasNoItems:            "订单缺少商品项，数据不完整",
	ErrStatusTransition:           "订单当前状态不支持该操作",
	ErrOrderNotCancellable:        "只有未发货的订单可以取消",
	ErrOrderNotRefundable:         "只有已付款的订单可以退款",
	ErrAlreadyCancelled:           "订单已取消，不能再操作",
	ErrAlreadyRefunded:            "订单已退款，不能再操作",
	ErrCancelReasonRequired:       "请填写取消原因",
	ErrPaymentMethodRequired:      "缺少支付方式",
	ErrPaymentChannelFailed:       "支付未成功，请稍后重试",
	ErrReturnNotFound:             "退货申请不存在",
	ErrReturnItemsRequired:        "请至少选择一件要退的商品",
	ErrReturnQuantityInvalid:      "退货数量必须是正整数",
	ErrReturnQuantityExceeded:     "退货数量超过可退数量",
	ErrReturnReasonRequired:       "请说明退货原因",
	ErrReturnNotCancellable:       "该申请已进入处理流程，无法撤销",
	ErrReturnOrderNotReturnable:   "该订单当前状态不支持退货",
	ErrReturnNotReviewable:        "该申请不在待审核状态",
	ErrReturnNotReceivable:        "该申请不在待收货状态（或已完成）",
	ErrReturnRejectReasonRequired: "请填写拒绝原因",
	ErrVariantNotFound:            "商品规格不存在或已下架",
	ErrStockInsufficient:          "库存不足，无法下单",
	ErrStockUnavailable:           "库存服务暂时不可用，请稍后重试",
	ErrCouponNotFound:             "优惠码不存在",
	ErrCouponCodeRequired:         "请填写优惠码",
	ErrCouponCodeTaken:            "这个优惠码已经存在",
	ErrCouponTypeInvalid:          "优惠类型只支持按比例折扣或固定金额",
	ErrCouponValueInvalid:         "优惠值不合法",
	ErrCouponWindowInvalid:        "生效时间不合法",
	ErrCouponDisabled:             "优惠码已停用",
	ErrCouponNotStarted:           "优惠码尚未开始",
	ErrCouponExpired:              "优惠码已过期",
	ErrCouponExhausted:            "优惠码已被用完",
	ErrCouponUserLimit:            "你已使用过该优惠码",
	ErrCouponMinSubtotal:          "订单金额未达到该优惠码的使用门槛",
	ErrCouponInUse:                "该优惠码已有核销记录，不能删除",
}

func Err(key string) error {
	err := error(facing.New(key, errFallbacks[key]))
	if key == ErrReturnRequestConflict {
		return facing.WithStatus(err, http.StatusConflict)
	}
	if key == ErrOrderNotFound {
		return facing.WithStatus(err, http.StatusNotFound)
	}
	return err
}

var UserFacingMessages = []string{
	MsgCreateSuccess, MsgStatusChanged, MsgCancelled, MsgRefunded, MsgPaid,
	ErrInvalidParam, ErrProjectRequired, ErrItemsRequired, ErrItemLimitExceeded,
	ErrQuantityInvalid, ErrCustomerEmailRequired, ErrCustomerEmailInvalid,
	ErrOrderNoInvalid, ErrStatusInvalid,

	ErrCurrencyUnknown,
	ErrOrderNotFound, ErrOrderNoTaken, ErrOrderHasNoItems,
	ErrStatusTransition, ErrOrderNotCancellable, ErrOrderNotRefundable,
	ErrAlreadyCancelled, ErrAlreadyRefunded, ErrCancelReasonRequired,
	ErrPaymentMethodRequired, ErrPaymentChannelFailed,
	ErrVariantNotFound, ErrStockInsufficient, ErrStockUnavailable,

	ErrOrderNotShippable, ErrShipQuantityExceeded, ErrShipLinesRequired,
	ErrShipWarehouseRequired, ErrShipRequestKeyConflict,
	ErrCarrierNotFound, ErrCarrierCodeTaken, ErrCarrierTemplateInvalid, ErrCarrierStatusInvalid,

	ErrCouponNotFound, ErrCouponCodeRequired, ErrCouponDisabled,
	ErrCouponNotStarted, ErrCouponExpired, ErrCouponExhausted,
	ErrCouponUserLimit, ErrCouponMinSubtotal,

	MsgReturnRequested, MsgReturnCancelled,
	ErrReturnItemsRequired, ErrReturnQuantityInvalid, ErrReturnQuantityExceeded,
	ErrReturnReasonRequired, ErrReturnNotCancellable, ErrReturnOrderNotReturnable, ErrReturnRequestConflict,
	ErrInternal,
}

const (
	MsgCouponCreated = "order.msg.couponCreated"
	MsgCouponUpdated = "order.msg.couponUpdated"
	MsgCouponDeleted = "order.msg.couponDeleted"

	ErrCouponNotFound      = "order.err.couponNotFound"
	ErrCouponCodeRequired  = "order.err.couponCodeRequired"
	ErrCouponCodeTaken     = "order.err.couponCodeTaken"
	ErrCouponTypeInvalid   = "order.err.couponTypeInvalid"
	ErrCouponValueInvalid  = "order.err.couponValueInvalid"
	ErrCouponWindowInvalid = "order.err.couponWindowInvalid"
	ErrCouponDisabled      = "order.err.couponDisabled"
	ErrCouponNotStarted    = "order.err.couponNotStarted"
	ErrCouponExpired       = "order.err.couponExpired"
	ErrCouponExhausted     = "order.err.couponExhausted"
	ErrCouponUserLimit     = "order.err.couponUserLimit"
	ErrCouponMinSubtotal   = "order.err.couponMinSubtotal"
	ErrCouponInUse         = "order.err.couponInUse"
)

const (
	BulkNoneSelected = "order.bulk.noneSelected"
	BulkAllDone      = "order.bulk.allDone"
	BulkAllSkipped   = "order.bulk.allSkipped"
	BulkPartial      = "order.bulk.partial"

	BulkVerbFlowed    = "order.bulk.verb.flowed"
	BulkVerbCancelled = "order.bulk.verb.cancelled"
	BulkVerbApproved  = "order.bulk.verb.approved"
	BulkVerbRejected  = "order.bulk.verb.rejected"
	BulkVerbDeleted   = "order.bulk.verb.deleted"
	BulkVerbDisabled  = "order.bulk.verb.disabled"
	BulkVerbEnabled   = "order.bulk.verb.enabled"

	BulkNounOrder  = "order.bulk.noun.order"
	BulkNounReturn = "order.bulk.noun.return"
	BulkNounCoupon = "order.bulk.noun.coupon"

	BulkCouponTargetInvalid = "order.bulk.couponTargetInvalid"

	BulkCancelReasonRequired = "order.bulk.cancelReasonRequired"

	ErrBulkRejected = "order.err.bulkRejected"

	BulkReturnRejectReasonRequired = "order.bulk.returnRejectReasonRequired"
)

const OrderStatusKeyPrefix = "site.fragment.order.status."

func OrderStatusLabel(status string) (key, fallback string) {
	switch strings.TrimSpace(status) {
	case "pending":
		return OrderStatusKeyPrefix + "pending", "待付款"
	case "paid":
		return OrderStatusKeyPrefix + "paid", "已付款"
	case "shipped":
		return OrderStatusKeyPrefix + "shipped", "已发货"
	case "completed":
		return OrderStatusKeyPrefix + "completed", "已完成"
	case "cancelled":
		return OrderStatusKeyPrefix + "cancelled", "已取消"
	case "refunded":
		return OrderStatusKeyPrefix + "refunded", "已退款"
	case "":
		return "", "—"
	default:
		return "", status
	}
}

func OrderStatusTone(status string) string {
	switch strings.TrimSpace(status) {
	case "pending":
		return "warn"
	case "paid", "shipped":
		return "info"
	case "completed":
		return "ok"
	case "refunded":
		return "danger"
	default:
		return "mute"
	}
}

const CreatedViaKeyPrefix = "admin.orders.created_via."

func CreatedViaLabel(v string) (key, fallback string) {
	switch strings.TrimSpace(v) {
	case "checkout":
		return CreatedViaKeyPrefix + "checkout", "访客结算"
	case "admin":
		return CreatedViaKeyPrefix + "admin", "后台自建"
	case "api":
		return CreatedViaKeyPrefix + "api", "接口"
	case "":
		return "", "—"
	default:
		return "", v
	}
}

const FulfillmentKeyPrefix = "admin.orders.fulfillment."

var FulfillmentStatuses = []string{
	"pending", "paid", "partial", "shipped", "received", "returned",
	"return_done", "reship", "exception", "failed", "cancelled", "refunded",
}

func FulfillmentLabel(v string) (key, fallback string) {
	switch strings.TrimSpace(v) {
	case "pending":
		return FulfillmentKeyPrefix + "pending", "待履约"
	case "paid":
		return FulfillmentKeyPrefix + "paid", "待发货"
	case "partial":
		return FulfillmentKeyPrefix + "partial", "部分发货"
	case "shipped":
		return FulfillmentKeyPrefix + "shipped", "已发货"
	case "received":
		return FulfillmentKeyPrefix + "received", "已收货"
	case "returned":
		return FulfillmentKeyPrefix + "returned", "已退货"
	case "return_done":
		return FulfillmentKeyPrefix + "return_done", "退货完成"
	case "reship":
		return FulfillmentKeyPrefix + "reship", "换货补发"
	case "exception":
		return FulfillmentKeyPrefix + "exception", "履约异常"
	case "failed":
		return FulfillmentKeyPrefix + "failed", "履约失败"
	case "cancelled":
		return FulfillmentKeyPrefix + "cancelled", "已取消"
	case "refunded":
		return FulfillmentKeyPrefix + "refunded", "已退款"
	case "":
		return "", "—"
	default:
		return "", v
	}
}

func FulfillmentTone(v string) string {
	switch strings.TrimSpace(v) {
	case "pending", "paid":
		return "warn"
	case "partial", "reship", "returned":
		return "info"
	case "shipped", "received", "return_done":
		return "ok"
	case "exception", "failed":
		return "danger"
	default:
		return "mute"
	}
}

const (
	ReturnStatusKeyRequested = "admin.returns.status.requested"
	ReturnStatusKeyApproved  = "admin.returns.status.approved"
	ReturnStatusKeyReceived  = "admin.returns.status.received"
	ReturnStatusKeyCompleted = "admin.returns.status.completed"
	ReturnStatusKeyRejected  = "admin.returns.status.rejected"
	ReturnStatusKeyCancelled = "admin.returns.status.cancelled"
)

func ReturnStatusLabel(status string) (key, fallback string) {
	switch strings.TrimSpace(status) {
	case "requested":
		return ReturnStatusKeyRequested, "待审核"
	case "approved":
		return ReturnStatusKeyApproved, "待收货"
	case "received":
		return ReturnStatusKeyReceived, "待退款"
	case "completed":
		return ReturnStatusKeyCompleted, "已完成"
	case "rejected":
		return ReturnStatusKeyRejected, "已拒绝"
	case "cancelled":
		return ReturnStatusKeyCancelled, "已撤销"
	case "":
		return "", "—"
	default:
		return "", status
	}
}

func ReturnStatusTone(status string) string {
	switch strings.TrimSpace(status) {
	case "requested", "approved", "received":
		return "warn"
	case "completed":
		return "ok"
	default:
		return "mute"
	}
}

const (
	CouponStateEnabled    = "enabled"
	CouponStateDisabled   = "disabled"
	CouponStateExpired    = "expired"
	CouponStateNotStarted = "not_started"
	CouponStateExhausted  = "exhausted"
)

func CouponStateLabel(state string) (key, fallback string) {
	switch strings.TrimSpace(state) {
	case CouponStateEnabled:
		return "admin.coupons.status.enabled", "生效中"
	case CouponStateDisabled:
		return "admin.coupons.status.disabled", "已停用"
	case CouponStateExpired:
		return "admin.coupons.status.expired", "已过期"
	case CouponStateNotStarted:
		return "admin.coupons.status.not_started", "未开始"
	case CouponStateExhausted:
		return "admin.coupons.status.exhausted", "已用完"
	case "":
		return "", "—"
	default:
		return "", state
	}
}

func CouponStateTone(state string) string {
	switch strings.TrimSpace(state) {
	case CouponStateEnabled:
		return "ok"
	case CouponStateExpired, CouponStateExhausted:
		return "danger"
	case CouponStateNotStarted:
		return "warn"
	default:
		return "mute"
	}
}

const (
	OrderNewOptionAvailable = "admin.order_new.option.available"

	CouponsHeading  = "admin.coupons.heading"
	OrdersHeading   = "admin.orders.heading"
	ReturnsHeading  = "admin.returns.heading"
	OrderSalesTitle = "admin.order.sales.title"

	PendingShipTitle   = "admin.order.pendingShip.title"
	OrderCarriersTitle = "admin.order.carriers.title"

	CouponFormInvalidID      = "admin.coupons.form.invalid_id"
	CouponFormForeignProject = "admin.coupons.form.foreign_project"

	OrderNewFieldCountryNone = "admin.order_new.field.country_none"

	OrderNewFieldCurrency    = "admin.order_new.field.currency"
	OrderNewFieldCurrencyUse = "admin.order_new.field.currency_default"
	OrderNewHintCurrency     = "admin.order_new.hint.currency"
)

func FallbackFor(key string) string { return errFallbacks[key] }
