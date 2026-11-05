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

func Err(key string) error {
	err := error(facing.New(key))
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
