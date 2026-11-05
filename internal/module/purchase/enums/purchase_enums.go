package purchaseenums

import (
	"fmt"
	"net/http"
	"strings"

	"go_wp/pkg/facing"
)

const (
	MsgCreateSuccess = "MsgCreateSuccess"
	MsgUpdateSuccess = "MsgUpdateSuccess"
	MsgDeleteSuccess = "MsgDeleteSuccess"
	MsgListSuccess   = "MsgListSuccess"
	MsgDetailSuccess = "MsgDetailSuccess"

	MsgReceiptSuccess = "MsgReceiptSuccess"
)

const (
	ErrInvalidParam = "ErrInvalidParam"

	ErrSourceNotFound      = "ErrSourceNotFound"
	ErrSourceNameRequired  = "ErrSourceNameRequired"
	ErrSourceCodeRequired  = "ErrSourceCodeRequired"
	ErrSourceCodeInvalid   = "ErrSourceCodeInvalid"
	ErrSourceCodeTaken     = "ErrSourceCodeTaken"
	ErrSourceTypeInvalid   = "ErrSourceTypeInvalid"
	ErrSourceStatusInvalid = "ErrSourceStatusInvalid"

	ErrSourceInternalNotRelated = "ErrSourceInternalNotRelated"

	ErrSourceSettleNotInternal = "ErrSourceSettleNotInternal"
	ErrSourceSettleInvalid     = "ErrSourceSettleInvalid"
	ErrSourceConfigInvalid     = "ErrSourceConfigInvalid"
	ErrSourceFilterInvalid     = "ErrSourceFilterInvalid"

	ErrPurchaseOrderNotFound   = "ErrPurchaseOrderNotFound"
	ErrPurchaseCodeRequired    = "ErrPurchaseCodeRequired"
	ErrPurchaseCodeInvalid     = "ErrPurchaseCodeInvalid"
	ErrPurchaseCodeTaken       = "ErrPurchaseCodeTaken"
	ErrPurchaseSourceRequired  = "ErrPurchaseSourceRequired"
	ErrPurchaseSourceDisabled  = "ErrPurchaseSourceDisabled"
	ErrPurchaseLinesRequired   = "ErrPurchaseLinesRequired"
	ErrPurchaseLinesTooMany    = "ErrPurchaseLinesTooMany"
	ErrPurchaseLineRequired    = "ErrPurchaseLineRequired"
	ErrPurchaseLineDuplicate   = "ErrPurchaseLineDuplicate"
	ErrPurchaseQuantityInvalid = "ErrPurchaseQuantityInvalid"
	ErrPurchasePriceInvalid    = "ErrPurchasePriceInvalid"
	ErrPurchaseStatusInvalid   = "ErrPurchaseStatusInvalid"
	ErrPurchaseLinesLocked     = "ErrPurchaseLinesLocked"
	ErrPurchaseLineNotFound    = "ErrPurchaseLineNotFound"

	ErrReceiptLinesRequired   = "ErrReceiptLinesRequired"
	ErrReceiptLineDuplicate   = "ErrReceiptLineDuplicate"
	ErrReceiptQuantityInvalid = "ErrReceiptQuantityInvalid"

	ErrReceiptOverReceive    = "ErrReceiptOverReceive"
	ErrReceiptOrderDone      = "ErrReceiptOrderDone"
	ErrReceiptRequestInvalid = "ErrReceiptRequestInvalid"

	ErrProductionSourceNotInternal = "ErrProductionSourceNotInternal"
	ErrProductionVariantRequired   = "ErrProductionVariantRequired"
	ErrProductionCostInvalid       = "ErrProductionCostInvalid"

	ErrVariantCostPortMissing = "ErrVariantCostPortMissing"

	ErrPurchaseStatusNotFinishable    = "ErrPurchaseStatusNotFinishable"
	ErrPurchaseStatusNotCancelable    = "ErrPurchaseStatusNotCancelable"
	ErrPurchaseStatusNotPlaceable     = "ErrPurchaseStatusNotPlaceable"
	ErrPurchaseStatusNotExceptionable = "ErrPurchaseStatusNotExceptionable"
	ErrPurchaseStatusNotResumable     = "ErrPurchaseStatusNotResumable"
	ErrPurchaseStatusNotReceivable    = "ErrPurchaseStatusNotReceivable"

	ErrStockSKURequired = "ErrStockSKURequired"
)

const (
	SourceTypeExternal = "external"
	SourceTypeInternal = "internal"
)

const (
	SourceStatusActive   = "active"
	SourceStatusDisabled = "disabled"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

const (
	PurchaseStatusDraft = "draft"

	PurchaseStatusOrdered   = "ordered"
	PurchaseStatusPartial   = "partial"
	PurchaseStatusReceived  = "received"
	PurchaseStatusException = "exception"

	PurchaseStatusFinished = "finished"

	PurchaseStatusCancelled = "cancelled"
)

var PurchaseDerivedStatuses = []string{
	PurchaseStatusOrdered, PurchaseStatusPartial, PurchaseStatusReceived,
}

var PurchaseReceivableStatuses = []string{
	PurchaseStatusOrdered, PurchaseStatusPartial,
}

var PurchaseTerminalStatuses = []string{
	PurchaseStatusFinished, PurchaseStatusCancelled,
}

var PurchaseStatuses = []string{
	PurchaseStatusDraft, PurchaseStatusOrdered, PurchaseStatusPartial,
	PurchaseStatusReceived, PurchaseStatusException,
	PurchaseStatusFinished, PurchaseStatusCancelled,
}

func IsKnownPurchaseStatus(status string) bool {
	for _, s := range PurchaseStatuses {
		if s == status {
			return true
		}
	}
	return false
}

func IsReceivableStatus(status string) bool {
	for _, s := range PurchaseReceivableStatuses {
		if s == status {
			return true
		}
	}
	return false
}

const (
	ReceiptKindPurchase   = "purchase"
	ReceiptKindProduction = "production"
)

const (
	ReceiptStatusPending = "pending"
	ReceiptStatusPosted  = "posted"
)

const (
	DirectionIn = "in"
)

const ErrStockLinesTooMany = "ErrStockLinesTooMany"

const (
	MovementSourcePurchaseOrder = "purchase_order"
	MovementSourceProduction    = "production"
)

const (
	ReasonCodePurchaseIn   = "purchase_in"
	ReasonCodeProductionIn = "production_in"
)

const (
	MsgInventorySourcesTitle = "MsgInventorySourcesTitle"

	MsgInventoryPurchasesTitle = "MsgInventoryPurchasesTitle"
)

var errStatuses = map[string]int{

	ErrSourceNotFound:        http.StatusNotFound,
	ErrPurchaseOrderNotFound: http.StatusNotFound,
	ErrPurchaseLineNotFound:  http.StatusNotFound,

	ErrSourceCodeTaken:                http.StatusConflict,
	ErrPurchaseCodeTaken:              http.StatusConflict,
	ErrPurchaseSourceDisabled:         http.StatusConflict,
	ErrPurchaseLinesLocked:            http.StatusConflict,
	ErrPurchaseStatusNotFinishable:    http.StatusConflict,
	ErrPurchaseStatusNotCancelable:    http.StatusConflict,
	ErrPurchaseStatusNotPlaceable:     http.StatusConflict,
	ErrPurchaseStatusNotExceptionable: http.StatusConflict,
	ErrPurchaseStatusNotResumable:     http.StatusConflict,
	ErrPurchaseStatusNotReceivable:    http.StatusConflict,
	ErrReceiptOverReceive:             http.StatusConflict,
	ErrReceiptOrderDone:               http.StatusConflict,
}

func Err(key string) error {
	return facing.WithStatus(facing.New(key), errStatuses[key])
}

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

