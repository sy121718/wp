package orderdto

import (
	"strconv"
	"strings"
)

// 表单请求：后台普通界面提交唯一入口是 HTMX 4，标准表单编码。
// 结构体上同时保留 form / json 标签是因为数据 API 仍按同一份字段名提供 JSON 载荷；
// 界面侧只由 FormPost 绑定，不自动猜测协议。

// OrderStatusForm 是订单状态流转的请求：单张用 orderId，批量用 ids（同一端点两种形状）。
type OrderStatusForm struct {
	OrderID  uint64   `form:"orderId" json:"orderId"`
	OrderIDs []string `form:"ids" json:"ids"`
	ToStatus string   `form:"toStatus" json:"toStatus"`
	Remark   string   `form:"remark" json:"remark"`
}

// OrderCancelForm 是订单取消的请求：单张用 orderId，批量用 ids（备注即取消原因）。
type OrderCancelForm struct {
	OrderID  uint64   `form:"orderId" json:"orderId"`
	OrderIDs []string `form:"ids" json:"ids"`
	Reason   string   `form:"reason" json:"reason"`
	Remark   string   `form:"remark" json:"remark"`
}

// OrderRefundForm 是单张订单的退款。
type OrderRefundForm struct {
	OrderID       uint64 `form:"orderId" json:"orderId"`
	Reason        string `form:"reason" json:"reason"`
	TransactionID string `form:"transactionId" json:"transactionId"`
}

// OrderNoteForm 是后台备注：只改备注，不动状态。
type OrderNoteForm struct {
	OrderID   uint64 `form:"orderId" json:"orderId"`
	AdminNote string `form:"adminNote" json:"adminNote"`
}

// OrderBulkIDs 归一化订单 ID：去空白、丢空串。
func OrderBulkIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ParseOrderID 把表单里的订单号转成 uint64；空串或非法返回 0。
func ParseOrderID(text string) uint64 {
	id, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// ReturnReviewForm 是退货审核（通过 / 驳回共用同一组字段）：单条用 returnId，批量用 ids。
type ReturnReviewForm struct {
	ReturnID      uint64   `form:"returnId" json:"returnId"`
	ReturnIDs     []string `form:"ids" json:"ids"`
	Remark        string   `form:"remark" json:"remark"`
	AutoReceive   bool     `form:"autoReceive" json:"autoReceive"`
	WarehouseID   string   `form:"warehouseId" json:"warehouseId"`
	TransactionID string   `form:"transactionId" json:"transactionId"`
}

// ReturnReceiveForm 是退货收货。
type ReturnReceiveForm struct {
	ReturnID      uint64 `form:"returnId" json:"returnId"`
	WarehouseID   string `form:"warehouseId" json:"warehouseId"`
	Remark        string `form:"remark" json:"remark"`
	TransactionID string `form:"transactionId" json:"transactionId"`
}

// CouponDeleteForm 是优惠码删除：单张用 id，批量用 ids。
type CouponDeleteForm struct {
	ID        uint64   `form:"id" json:"id"`
	IDs       []string `form:"ids" json:"ids"`
	ProjectID string   `form:"projectId" json:"projectId"`
}

// CouponBulkDeleteReq 是批量删除优惠码的明确入口：整批同事务，合法重复 id 去重。
type CouponBulkDeleteReq struct {
	IDs       []string `form:"ids" json:"ids"`
	ProjectID string   `form:"projectId" json:"projectId"`
}

// CouponBulkStatusForm 是批量启停的表单形状：status 用字符串承载，
// 以便「未提供 / 空串 / 非法值」都与合法的 "0" / "1" 区分开。
type CouponBulkStatusForm struct {
	IDs       []string `form:"ids" json:"ids"`
	Status    string   `form:"status" json:"status"`
	ProjectID string   `form:"projectId" json:"projectId"`
}

// CouponBulkStatusReq 是批量启停的明确入口：只改 status，不整行覆盖其它字段。
type CouponBulkStatusReq struct {
	IDs []string

	ProjectID string

	Status int

	OperatorID uint64
}

// CarrierStatusForm 是承运商的启停（沿用 update 端点，只提交 id 与 status）。
type CarrierStatusForm struct {
	ID        string `form:"id" json:"id"`
	ProjectID string `form:"projectId" json:"projectId"`
	Status    string `form:"status" json:"status"`
}
