package orderdto

import (
	"go_wp/pkg/utils"
)

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

	Returnable int `json:"returnable"`
}

type ReturnResp struct {
	ID          uint64 `json:"id"`
	ProjectID   string `json:"projectId"`
	OrderID     uint64 `json:"orderId"`
	OrderNo     string `json:"orderNo"`
	ReturnNo    string `json:"returnNo"`
	Status      string `json:"status"`
	StatusLabel string `json:"statusLabel"`

	StatusTone string `json:"-"`
	Reason     string `json:"reason"`

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

	OriginalWarehouseName string `json:"originalWarehouseName"`

	PendingInboundLines int               `json:"pendingInboundLines"`
	Items               []*ReturnItemResp `json:"items"`
	CreateTime          utils.JSONTime    `json:"createTime"`
	UpdateTime          utils.JSONTime    `json:"updateTime"`
}

type ReturnDetailResp struct {
	Return *ReturnResp `json:"return"`
	Order  *OrderResp  `json:"order"`
}

type ReturnableItem struct {
	OrderItemID uint64 `json:"orderItemId"`

	Returnable int `json:"returnable"`

	Quantity int `json:"quantity"`
}

type ReturnableResp struct {
	CanRequest bool              `json:"canRequest"`
	Items      []*ReturnableItem `json:"items"`

	ReturnableTotal int `json:"returnableTotal"`
}

type ReturnListResp struct {
	List   []*ReturnResp    `json:"list"`
	Total  int64            `json:"total"`
	Counts map[string]int64 `json:"counts"`
}
