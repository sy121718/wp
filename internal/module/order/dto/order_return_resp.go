package orderdto

// order_return_resp.go — 退货入库的响应（BIZ-1）。
//
// 金额与展示文案都由服务端算好（分与元的换算只在一处发生）；
// StatusLabel 也是服务端给的 —— 同一个状态在后台页、访客片段、邮件里必须是同一句话。

import "time"

// ReturnItemResp 退货明细视图。
type ReturnItemResp struct {
	OrderItemID      uint64 `json:"orderItemId"`
	ProductID        string `json:"productId"`
	VariantID        string `json:"variantId"`
	ProductName      string `json:"productName"`
	VariantLabel     string `json:"variantLabel"`
	SKU              string `json:"sku"`
	UnitPrice        int64  `json:"unitPrice"`
	UnitPriceLabel   string `json:"unitPriceLabel"`
	Quantity         int    `json:"quantity"`
	ReceivedQuantity int    `json:"receivedQuantity"`
	RefundAmount     int64  `json:"refundAmount"`
	RefundLabel      string `json:"refundLabel"`
	// Returnable 该订单项**还能退多少**（购买数量 − 其它未撤销申请已占用的数量）。
	// 前台据此决定「申请退货」表单里每行的最大可填值，后台据此决定按钮是否可用。
	Returnable int `json:"returnable"`
}

// ReturnResp 退货单视图。
type ReturnResp struct {
	ID          uint64 `json:"id"`
	ProjectID   string `json:"projectId"`
	OrderID     uint64 `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	ReturnNo    string `json:"returnNo"`
	Status      string `json:"status"`
	StatusLabel string `json:"statusLabel"`
	Reason      string `json:"reason"`
	// RefundAmount 整单的退款额（明细之和），单位分。
	RefundAmount  int64             `json:"refundAmount"`
	RefundLabel   string            `json:"refundLabel"`
	UserID        *uint64           `json:"userId"`
	CustomerEmail string            `json:"customerEmail"`
	CustomerName  string            `json:"customerName"`
	AdminNote     string            `json:"adminNote"`
	ReviewerName  string            `json:"reviewerName"`
	ReviewedAt    *time.Time        `json:"reviewedAt"`
	ReceivedAt    *time.Time        `json:"receivedAt"`
	RefundedAt    *time.Time        `json:"refundedAt"`
	TransactionID string            `json:"transactionId"`
	Items         []*ReturnItemResp `json:"items"`
	CreateTime    time.Time         `json:"createTime"`
	UpdateTime    time.Time         `json:"updateTime"`
}

// ReturnDetailResp 退货单详情（含订单摘要：审核时不可能不看订单）。
type ReturnDetailResp struct {
	Return *ReturnResp `json:"return"`
	Order  *OrderResp  `json:"order"`
}

// ReturnableItem 某个订单项当前还能退多少。
type ReturnableItem struct {
	OrderItemID uint64 `json:"orderItemId"`
	// Returnable 还能退的数量（购买数量 − 其它未撤销申请已占用的数量）。
	Returnable int `json:"returnable"`
	// Quantity 购买数量（表单里显示「买了 3 件，可退 2 件」用）。
	Quantity int `json:"quantity"`
}

// ReturnableResp 订单的可退明细（访客侧渲染退货表单用）。
type ReturnableResp struct {
	Items []*ReturnableItem `json:"items"`
	// ReturnableTotal 整单还能退的总件数（0 表示这单已经没什么可退的了）。
	ReturnableTotal int `json:"returnableTotal"`
}

// ReturnListResp 列表结果。
type ReturnListResp struct {
	List   []*ReturnResp    `json:"list"`
	Total  int64            `json:"total"`
	Counts map[string]int64 `json:"counts"`
}
