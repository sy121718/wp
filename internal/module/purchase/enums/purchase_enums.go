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

var errFallbacks = map[string]string{
	ErrInvalidParam:                "参数错误",
	ErrSourceNotFound:              "货源不存在",
	ErrSourceNameRequired:          "货源名称必填",
	ErrSourceCodeRequired:          "货源编码必填",
	ErrSourceCodeInvalid:           "货源编码只允许大写字母 / 数字 / 下划线，且不超长",
	ErrSourceCodeTaken:             "同工程下货源编码已占用",
	ErrSourceTypeInvalid:           "类型只认 external / internal（决定是否按内部交易口径出报表）",
	ErrSourceStatusInvalid:         "状态不是 active / disabled",
	ErrSourceInternalNotRelated:    "内部货源必须标记关联方：内部交易必须能被关联方报表捕获。",
	ErrSourceSettleNotInternal:     "结算价只属于内部货源（自产商品的成本口径，外部供应商走采购价）。",
	ErrSourceSettleInvalid:         "结算价不能为负",
	ErrSourceConfigInvalid:         "对接配置必须是 JSON 对象",
	ErrSourceFilterInvalid:         "报表筛选参数不合法（关联方标志只认 true / false）",
	ErrPurchaseOrderNotFound:       "采购单不存在",
	ErrPurchaseCodeRequired:        "采购单号必填",
	ErrPurchaseCodeInvalid:         "采购单号只允许大写字母 / 数字 / 下划线 / 连字符，且不超长",
	ErrPurchaseCodeTaken:           "同工程下采购单号已占用",
	ErrPurchaseSourceRequired:      "采购单必须指定货源（来源 = 货源管理的货源）",
	ErrPurchaseSourceDisabled:      "已停用的货源不能下采购单（停用 = 不再选用）",
	ErrPurchaseLinesRequired:       "采购单至少要有一行",
	ErrPurchaseLinesTooMany:        "采购行数超过上限",
	ErrPurchaseLineRequired:        "采购行必须给出变体",
	ErrPurchaseLineDuplicate:       "同一采购单里同一个 SKU 重复出现",
	ErrPurchaseQuantityInvalid:     "采购数量必须为正整数",
	ErrPurchasePriceInvalid:        "采购单价必须为正数",
	ErrPurchaseStatusInvalid:       "状态筛选值不在合法状态内（草稿 / 已下单 / 部分到货 / 已到货 / 异常 / 已完结 / 已取消）",
	ErrPurchaseLinesLocked:         "已有入库数量的采购单不能再改行（改了状态推导就不成立）",
	ErrPurchaseLineNotFound:        "采购行不存在（或不属于该采购单）",
	ErrPurchaseStatusNotFinishable: "只有已到货或部分到货的采购单可以提前关单",
	ErrPurchaseStatusNotCancelable: "只有草稿 / 已下单 / 部分到货 / 异常中的采购单可以取消（已到货的货已进仓，要走退货）",
	ErrPurchaseStatusNotPlaceable:  "只有草稿可以下单（已下单的单重复下单不改变任何东西）",
	ErrPurchaseStatusNotExceptionable: "只有已下单 / 部分到货 / 已到货的采购单可以标记异常" +
		"（草稿还没下单，已完结 / 已取消的单已经收尾）",
	ErrPurchaseStatusNotResumable: "只有异常中的采购单可以解除异常",
	ErrPurchaseStatusNotReceivable: "只有已下单 / 部分到货的采购单可以登记收货" +
		"（草稿还没下单；已到货 / 已完结 / 已取消已收尾；异常要先解除）",
	ErrReceiptLinesRequired:        "入库清单不能为空",
	ErrReceiptLineDuplicate:        "同一入库请求里同一采购行只能出现一次",
	ErrReceiptQuantityInvalid:      "入库数量必须为正整数",
	ErrReceiptOverReceive:          "入库数量超过「采购数量 - 已入库数量」：超收在数据层即不可能。",
	ErrReceiptOrderDone:            "采购单已全部入库，无需再收",
	ErrReceiptRequestInvalid:       "幂等键不合法（超长 / 非法字符）",
	ErrProductionSourceNotInternal: "生产入库只认内部货源（自家工厂）：外部供应商走采购单。",
	ErrProductionVariantRequired:   "生产入库必须给出 SKU 变体",
	ErrProductionCostInvalid:       "生产入库的成本价必须手工填写且非负",
	ErrVariantCostPortMissing:      "商品侧成本价端口未注入（装配缺陷 / 纯采购单测路径）。",
	ErrStockSKURequired:            "缺少仓库侧 SKU 编码：请传裸码（不带仓码前缀，如 DRAWERSMOKE_001）",
	ErrStockLinesTooMany:           "变动清单行数超过单批上限",
}

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
