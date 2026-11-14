package orderdto

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type ShipLineReq struct {
	OrderItemID uint64 `form:"orderItemId" json:"orderItemId" binding:"required"`

	WarehouseID string `form:"warehouseId" json:"warehouseId"`
	Quantity    int    `form:"quantity" json:"quantity"`
}

// 发货明细的三个线上键（lines[][orderItemId] / lines[][quantity] /
// lines[][warehouseId]）都是不带动态下标的字面量，字段门禁才能静态对账。
//
// 每行由模板按 DOM 顺序同时提交这三个控件（两个 hidden + 一个数字输入），
// url.Values 对同一个键保留出现顺序，因此按下标配对即可还原行。
type ShipOrderReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
	RequestID string `form:"requestId" json:"requestId"`

	CarrierID   string `form:"carrierId" json:"carrierId"`
	CarrierName string `form:"carrierName" json:"carrierName"`
	TrackingNo  string `form:"trackingNo" json:"trackingNo"`
	Remark      string `form:"remark" json:"remark"`

	OperatorID   int64  `form:"operatorId" json:"operatorId"`
	OperatorName string `form:"operatorName" json:"operatorName"`

	// 三个平行数组是线上字段；Lines 由 BindForm 装配，不属于线上键。
	// 不给 Lines 加 binding:"required"：gin 的校验在 BindForm 钩子之前跑，
	// 那时 Lines 还是 nil，会把合法表单整单拒掉；空明细由 planShipment 报
	// ErrShipLinesRequired。
	LineItemIDs      []string      `form:"lines[][orderItemId]" json:"-"`
	LineQuantities   []string      `form:"lines[][quantity]" json:"-"`
	LineWarehouseIDs []string      `form:"lines[][warehouseId]" json:"-"`
	Lines            []ShipLineReq `form:"-" json:"lines"`
}

// BindForm 把三个平行数组按出现顺序配对成 []ShipLineReq。
//
// OrderItemID 仍是每行显式提交的值（hidden 控件），不按下标猜归属；
// 下标只用于把同一行的数量与仓库配到那个 id 上。
//
// 三条语义（与 planShipment 的分工一致，service 侧不需要改）：
//  1. 所有行都没填 → Lines 为空，planShipment 报 ErrShipLinesRequired；
//  2. 部分行有值 → 只收有值的行，其余忽略（部分发货的常规操作）；
//  3. 显式填 0 → 保留，由 planShipment 的 Quantity <= 0 报 ErrQuantityInvalid。
//
// 数组长度不一致时整单报错、不静默配对：缺一个 hidden 控件就会让后面的数量
// 串到前一行的订单项上，宁可直接拒绝也不发错货。
func (r *ShipOrderReq) BindForm(vals url.Values) error {
	if len(r.LineItemIDs) != len(r.LineQuantities) || len(r.LineItemIDs) != len(r.LineWarehouseIDs) {
		return errShipLineArraysMismatch
	}
	lines := make([]ShipLineReq, 0, len(r.LineItemIDs))
	for i, rawID := range r.LineItemIDs {
		itemID, err := strconv.ParseUint(strings.TrimSpace(rawID), 10, 64)
		if err != nil || itemID == 0 {
			continue
		}
		// 空串与非法值都当"这行没填数量"，不在这里报错，也不在这里判 0。
		quantity, qerr := strconv.Atoi(strings.TrimSpace(r.LineQuantities[i]))
		if qerr != nil {
			continue
		}
		lines = append(lines, ShipLineReq{
			OrderItemID: itemID,
			WarehouseID: strings.TrimSpace(r.LineWarehouseIDs[i]),
			Quantity:    quantity,
		})
	}
	if len(lines) > 0 {
		r.Lines = lines
	}
	return nil
}

// errShipLineArraysMismatch 是行数组长度不一致的内部错误：由绑定入口转成参数错，
// 不把实现细节交给界面。
var errShipLineArraysMismatch = errors.New("ship line arrays length mismatch")

type ListShipmentReq struct {
	OrderID   uint64 `form:"orderId" json:"orderId" binding:"required"`
	ProjectID string `form:"projectId" json:"projectId"`
}

type CarrierCreateReq struct {
	ProjectID           string `form:"projectId" json:"projectId"`
	Code                string `form:"code" json:"code" binding:"required"`
	Name                string `form:"name" json:"name" binding:"required"`
	TrackingURLTemplate string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Sort                int    `form:"sort" json:"sort"`
}

type CarrierUpdateReq struct {
	ID        string  `form:"id" json:"id" binding:"required"`
	ProjectID string  `form:"projectId" json:"projectId"`
	Code      *string `form:"code" json:"code"`
	Name      *string `form:"name" json:"name"`

	TrackingURLTemplate *string `form:"trackingUrlTemplate" json:"trackingUrlTemplate"`
	Status              *string `form:"status" json:"status"`
	Sort                *int    `form:"sort" json:"sort"`
}

type ListCarrierReq struct {
	ProjectID string `form:"projectId" json:"projectId"`

	IncludeDisabled bool `form:"includeDisabled" json:"includeDisabled"`
}

type PendingShipReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Keyword   string `form:"keyword" json:"keyword"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

type ShipmentPageQueryReq struct {
	ProjectID string `form:"project" json:"project"`
	Keyword   string `form:"keyword" json:"keyword"`
	Page      int    `form:"page" json:"page"`
	Limit     int    `form:"limit" json:"limit"`
}
