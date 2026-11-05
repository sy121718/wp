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

func Err(key string) error {
	status := http.StatusBadRequest
	switch key {
	case ErrInternal:
		status = http.StatusInternalServerError
	case ErrVariantNotFound, ErrCartItemAbsent, ErrCallbackOrderMissing:
		status = http.StatusNotFound
	}
	return facing.WithStatus(facing.New(key), status)
}

