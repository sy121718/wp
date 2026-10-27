package orderdto

// order_return_resp.go — 退货入库的响应（BIZ-1）。
//
// 金额与展示文案都由服务端算好（分与元的换算只在一处发生）；
// StatusLabel 也是服务端给的 —— 同一个状态在后台页、访客片段、邮件里必须是同一句话。

import (
	"go_wp/pkg/utils"
)

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
	// StatusTone 状态徽标分档（只给服务端模板用，json:"-"）：由 orderenums.ReturnStatusTone 唯一给出。
	StatusTone string `json:"-"`
	Reason     string `json:"reason"`
	// RefundAmount 整单的退款额（明细之和），单位分。
	RefundAmount  int64           `json:"refundAmount"`
	RefundLabel   string          `json:"refundLabel"`
	UserID        *uint64         `json:"userId"`
	CustomerEmail string          `json:"customerEmail"`
	CustomerName  string          `json:"customerName"`
	AdminNote     string          `json:"adminNote"`
	ReviewerName  string          `json:"reviewerName"`
	ReviewedAt    *utils.JSONTime `json:"reviewedAt"`
	ReceivedAt    *utils.JSONTime `json:"receivedAt"`
	RefundedAt    *utils.JSONTime `json:"refundedAt"`
	TransactionID string          `json:"transactionId"`
	// OriginalWarehouseName 原发货仓（仓库视角，B6-b）：这批货原本从哪个仓出去的，
	// 就该收回哪个仓。空串 = 这单还没发过货、下单时也没定仓（虚拟商品 / 无仓工程）。
	//
	// 只是**名字**：仓 id 由入库表单的仓库下拉回答，处理器不需要它。
	OriginalWarehouseName string `json:"originalWarehouseName"`
	// PendingInboundLines 待入库行数（明细里 received_quantity < quantity 的行数）。
	//
	// 仓库关心的是「还有几行没点完」，不是「还差几件」—— 一件一批的到货会被拆成多次，
	// 行数才对应收货台上的待办条数。件数差额另由明细表给（模板不做减法）。
	PendingInboundLines int               `json:"pendingInboundLines"`
	Items               []*ReturnItemResp `json:"items"`
	CreateTime          utils.JSONTime    `json:"createTime"`
	UpdateTime          utils.JSONTime    `json:"updateTime"`
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
