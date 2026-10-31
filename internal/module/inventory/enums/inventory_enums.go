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

var errFallbacks = map[string]string{
	ErrInvalidParam:                  "请求参数无效",
	ErrWarehouseNotFound:             "仓库不存在",
	ErrWarehouseNameRequired:         "仓库名称必填",
	ErrWarehouseCodeRequired:         "仓库短码必填",
	ErrWarehouseCodeInvalid:          "短码只允许字母与数字（SKU 编码前缀只允许 A-Z 0-9）",
	ErrWarehouseCodeTaken:            "同工程下短码已占用",
	ErrWarehouseStatusInvalid:        "状态不是 active / disabled",
	ErrWarehouseIsDefault:            "默认仓不能删除，也不能取消默认或停用",
	ErrWarehouseHasStock:             "仓内仍有非零库存，不能删除",
	ErrWarehouseDisabled:             "已停用的仓库不能作为归属仓",
	ErrWarehouseProjectMismatch:      "仓库不属于该工程",
	ErrWarehouseDefaultMissing:       "该工程没有默认仓可兜底，请先建一个仓库并设为默认仓",
	ErrWarehouseTypeInvalid:          "仓库类型不合法（仅支持自营 / 第三方 / 虚拟）",
	ErrWarehouseConfigInvalid:        "第三方仓对接配置不合法",
	ErrWarehouseCredentialInvalid:    "API 凭据不合法（过长或含控制字符）",
	ErrWarehouseCredentialKeyMissing: "未配置敏感配置加密密钥（app.secret），凭据不能保存；可改用凭据引用名",
	ErrWarehouseTypeVirtualDefault:   "虚拟仓没有实体收发能力，不能作为默认仓",
	ErrStockRemarkRequired:           "库存调整必须填写备注（说明这次调整的依据）",
	ErrMovementTimeRangeInvalid:      "时间筛选不合法（格式不对，或起始晚于截止）",
	ErrStockNotFound:                 "库存记录不存在",
	ErrStockVariantRequired:          "生成库存记录必须给出变体",
	ErrStockWarehouseNeeded:          "生成库存记录必须给出仓库（或可兜底的默认仓）",
	ErrStockCostInvalid:              "成本价不合法（显式传入的成本必须是不小于 0 的数值）",
	ErrStockLinesRequired:            "变动清单不能为空",
	ErrStockLinesTooMany:             "变动清单行数超过单批上限",
	ErrStockDirectionInvalid:         "方向不是 in / out / adjust",
	ErrStockQuantityInvalid:          "数量不合法（入 / 出必须为正，调整不得为负）",
	ErrStockProductRequired:          "变动必须能确定商品（行内 / 已有库存记录）",
	ErrStockReasonRequired:           "变动原因必填",
	ErrStockSKURequired:              "缺少仓库侧 SKU 编码：请传裸码（不带仓码前缀，如 DRAWERSMOKE_001）",
	ErrStockInsufficient:             "可用量不足，扣减已整体拒绝",
	ErrStockCachePortMissing:         "商品侧库存缓存端口未注入（装配缺陷 / 纯库存单测路径）。",
	ErrStockQuantityRequired:         "跟踪库存时必须填写数量（0 表示已卖光，需要自己填出来，留空不等于 0）",
	ErrStockUntrackedQuantity:        "不跟踪（无限）的库存行不允许带数量：要写具体数量请先切换成跟踪库存",
	ErrReasonNotFound:                "变动原因不在字典里（不接受自由文本）",
	ErrReasonDirectionMismatch:       "原因方向与本次变动方向不符",
	ErrReasonCodeRequired:            "原因 code 必填",
	ErrReasonCodeInvalid:             "原因 code 只允许小写字母 / 数字 / 下划线",
	ErrReasonCodeTaken:               "同工程（或内置）下 code 已占用",
	ErrReasonNameRequired:            "原因名称必填",
	ErrReasonDirectionInvalid:        "原因方向必须是 in / out / adjust",
	ErrReasonBuiltin:                 "内置原因不可改名（只能停用）",
	ErrReasonStatusInvalid:           "原因状态不是 active / disabled",
	ErrTransferNotFound:              "调拨单不存在",
	ErrTransferLinesRequired:         "调拨单至少要有一行明细",
	ErrTransferSameWarehouse:         "源仓与目标仓不能是同一个",
	ErrItemNotInSourceWarehouse:      "这条库存行不属于源仓",
	ErrTransferStatusInvalid:         "调拨单当前状态不允许这个操作",
	ErrTransferRejectReasonRequired:  "驳回必须写明理由（申请人据此决定怎么改）",
	ErrTransferRejectReasonTooLong:   "驳回理由过长：最多 {max} 个字符",
	ErrAttachmentRequired:            "出货与签收都必须上传凭证附件",
	ErrAttachmentNotFound:            "凭证引用的附件不存在",
	ErrStockNotEnough:                "可售数量不足（被订单预占的部分不能调拨）",
	ErrSKUSourceInvalid:              "SKU 来源取值不合法：只支持「自己创建」与「从仓库选」",
	ErrWarehouseSKURequired:          "从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU",
	ErrWarehouseSKUNotFound:          "该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录",
	ErrWarehouseSKUCodeTaken:         "该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU",
	ErrExternalSKUInvalid:            "外部编码不合法：长度需在 128 个字符以内且不含控制字符",
	ErrExternalSKUProductConflict:    "该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）",
	ErrReserveUnderflow:              "要释放的预占数量超过了这一行已预占的数量",
	ErrStockStatusInvalid:            "货行状态只能是启用或停用",
	ErrStockFreightInvalid:           "运费不合法：必须是不小于 0 的数（0 表示免运费）",
	ErrStockFXRateInvalid:            "成本换算汇率不合法：必须大于 0；汇率未固化请留空，不要填 0",
	ErrStockNameTooLong:              "商品名称过长：最多 200 个字符",
	ErrStockBarcodeTooLong:           "条码过长：最多 64 个字符",
	ErrStockRemarkTooLong:            "备注过长：最多 500 个字符",
	ErrStockImageTooLong:             "图片地址过长：最多 512 个字符",
}

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
	return facing.WithStatus(facing.New(key, errFallbacks[key]), errStatuses[key])
}

func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}

func FallbackFor(key string) string {
	return errFallbacks[key]
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
