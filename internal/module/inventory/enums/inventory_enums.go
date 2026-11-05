package inventoryenums

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

	MsgChangeSuccess    = "MsgChangeSuccess"
	MsgDeductSuccess    = "MsgDeductSuccess"
	MsgSyncSuccess      = "MsgSyncSuccess"
	MsgReconcileSuccess = "MsgReconcileSuccess"
)

const (
	ErrInvalidParam = "ErrInvalidParam"

	ErrWarehouseNotFound        = "ErrWarehouseNotFound"
	ErrWarehouseNameRequired    = "ErrWarehouseNameRequired"
	ErrWarehouseCodeRequired    = "ErrWarehouseCodeRequired"
	ErrWarehouseCodeInvalid     = "ErrWarehouseCodeInvalid"
	ErrWarehouseCodeTaken       = "ErrWarehouseCodeTaken"
	ErrWarehouseStatusInvalid   = "ErrWarehouseStatusInvalid"
	ErrWarehouseIsDefault       = "ErrWarehouseIsDefault"
	ErrWarehouseHasStock        = "ErrWarehouseHasStock"
	ErrWarehouseDisabled        = "ErrWarehouseDisabled"
	ErrWarehouseProjectMismatch = "ErrWarehouseProjectMismatch"

	ErrWarehouseDefaultMissing = "ErrWarehouseDefaultMissing"

	ErrWarehouseTypeInvalid = "ErrWarehouseTypeInvalid"

	ErrWarehouseConfigInvalid = "ErrWarehouseConfigInvalid"

	ErrWarehouseCredentialInvalid = "ErrWarehouseCredentialInvalid"

	ErrWarehouseCredentialKeyMissing = "ErrWarehouseCredentialKeyMissing"

	ErrWarehouseTypeVirtualDefault = "ErrWarehouseTypeVirtualDefault"

	ErrStockRemarkRequired = "ErrStockRemarkRequired"

	ErrMovementTimeRangeInvalid = "ErrMovementTimeRangeInvalid"

	ErrStockNotFound        = "ErrStockNotFound"
	ErrStockVariantRequired = "ErrStockVariantRequired"
	ErrStockWarehouseNeeded = "ErrStockWarehouseNeeded"

	ErrStockCostInvalid = "ErrStockCostInvalid"

	ErrStockLinesRequired    = "ErrStockLinesRequired"
	ErrStockLinesTooMany     = "ErrStockLinesTooMany"
	ErrStockDirectionInvalid = "ErrStockDirectionInvalid"
	ErrStockQuantityInvalid  = "ErrStockQuantityInvalid"
	ErrStockProductRequired  = "ErrStockProductRequired"
	ErrStockReasonRequired   = "ErrStockReasonRequired"

	ErrStockSKURequired = "ErrStockSKURequired"

	ErrStockInsufficient = "ErrStockInsufficient"

	ErrStockCachePortMissing = "ErrStockCachePortMissing"

	ErrStockQuantityRequired = "ErrStockQuantityRequired"

	ErrStockUntrackedQuantity = "ErrStockUntrackedQuantity"

	ErrReasonNotFound          = "ErrReasonNotFound"
	ErrReasonDirectionMismatch = "ErrReasonDirectionMismatch"
	ErrReasonCodeRequired      = "ErrReasonCodeRequired"
	ErrReasonCodeInvalid       = "ErrReasonCodeInvalid"
	ErrReasonCodeTaken         = "ErrReasonCodeTaken"
	ErrReasonNameRequired      = "ErrReasonNameRequired"
	ErrReasonDirectionInvalid  = "ErrReasonDirectionInvalid"
	ErrReasonBuiltin           = "ErrReasonBuiltin"
	ErrReasonStatusInvalid     = "ErrReasonStatusInvalid"

	ErrTransferNotFound         = "ErrTransferNotFound"
	ErrTransferLinesRequired    = "ErrTransferLinesRequired"
	ErrTransferSameWarehouse    = "ErrTransferSameWarehouse"
	ErrItemNotInSourceWarehouse = "ErrItemNotInSourceWarehouse"
	ErrTransferStatusInvalid    = "ErrTransferStatusInvalid"

	ErrTransferRejectReasonRequired = "ErrTransferRejectReasonRequired"
	ErrTransferRejectReasonTooLong  = "ErrTransferRejectReasonTooLong"

	ErrAttachmentRequired = "ErrAttachmentRequired"

	ErrAttachmentNotFound = "ErrAttachmentNotFound"

	ErrStockNotEnough = "ErrStockNotEnough"

	ErrSKUSourceInvalid = "ErrSKUSourceInvalid"

	ErrWarehouseSKURequired = "ErrWarehouseSKURequired"

	ErrWarehouseSKUNotFound = "ErrWarehouseSKUNotFound"

	ErrWarehouseSKUCodeTaken = "ErrWarehouseSKUCodeTaken"

	ErrExternalSKUInvalid = "ErrExternalSKUInvalid"

	ErrExternalSKUProductConflict = "ErrExternalSKUProductConflict"

	ErrStockStatusInvalid = "ErrStockStatusInvalid"

	ErrStockFreightInvalid = "ErrStockFreightInvalid"

	ErrStockFXRateInvalid = "ErrStockFXRateInvalid"

	ErrStockNameTooLong    = "ErrStockNameTooLong"
	ErrStockBarcodeTooLong = "ErrStockBarcodeTooLong"
	ErrStockRemarkTooLong  = "ErrStockRemarkTooLong"
	ErrStockImageTooLong   = "ErrStockImageTooLong"

	ErrReserveUnderflow = "ErrReserveUnderflow"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

const (
	WarehouseTypeSelf       = "self"
	WarehouseTypeThirdParty = "third_party"
	WarehouseTypeVirtual    = "virtual"
)

const (
	WarehouseConfigKeyProvider = "provider"

	WarehouseConfigKeyExternalCode = "externalCode"

	WarehouseConfigKeyAddress = "address"

	WarehouseConfigKeyContact = "contact"

	WarehouseConfigKeyAllowsShipping = "allowsShipping"

	WarehouseConfigKeyAPICipher = "apiKeyCipher"

	WarehouseConfigKeySecretRef = "secretRef"
)

const CredentialMask = "****"

const (
	DirectionIn     = "in"
	DirectionOut    = "out"
	DirectionAdjust = "adjust"
)

const (
	ReasonManualAdjust = "manual_adjust"

	MovementSourceInventoryPage = "inventory_page"

	MovementSourceOrderShipment = "order_shipment"

	MovementSourceProductForm = "product_form"
)

var errStatuses = map[string]int{

	ErrWarehouseNotFound:    http.StatusNotFound,
	ErrWarehouseSKUNotFound: http.StatusNotFound,
	ErrStockNotFound:        http.StatusNotFound,
	ErrReasonNotFound:       http.StatusNotFound,

	ErrWarehouseCodeTaken:         http.StatusConflict,
	ErrWarehouseSKUCodeTaken:      http.StatusConflict,
	ErrWarehouseIsDefault:         http.StatusConflict,
	ErrWarehouseHasStock:          http.StatusConflict,
	ErrWarehouseDisabled:          http.StatusConflict,
	ErrStockInsufficient:          http.StatusConflict,
	ErrReasonCodeTaken:            http.StatusConflict,
	ErrReasonBuiltin:              http.StatusConflict,
	ErrTransferNotFound:           http.StatusNotFound,
	ErrTransferSameWarehouse:      http.StatusConflict,
	ErrItemNotInSourceWarehouse:   http.StatusConflict,
	ErrTransferStatusInvalid:      http.StatusConflict,
	ErrAttachmentRequired:         http.StatusBadRequest,
	ErrAttachmentNotFound:         http.StatusNotFound,
	ErrStockNotEnough:             http.StatusConflict,
	ErrExternalSKUProductConflict: http.StatusConflict,
	ErrReserveUnderflow:           http.StatusConflict,
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

const (
	DetailExternalSKUOwner = "admin.inventory.err.externalSkuOwner"

	DetailStockNameLimit    = "admin.inventory.err.stockNameLimit"
	DetailStockBarcodeLimit = "admin.inventory.err.stockBarcodeLimit"
	DetailStockRemarkLimit  = "admin.inventory.err.stockRemarkLimit"
	DetailStockImageLimit   = "admin.inventory.err.stockImageLimit"
)

var ErrDetailFallbacks = map[string]string{
	DetailExternalSKUOwner:  "该外部编码在本仓已属于商品 {name}",
	DetailStockNameLimit:    "上限 {max} 个字符",
	DetailStockBarcodeLimit: "上限 {max} 个字符",
	DetailStockRemarkLimit:  "上限 {max} 个字符",
	DetailStockImageLimit:   "上限 {max} 个字符",
}
