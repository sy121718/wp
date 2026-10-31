package orderdto

import "go_wp/pkg/utils"

type VisitorOrderResp struct {
	ID                 uint64          `json:"id"`
	OrderNo            string          `json:"orderNo"`
	Status             string          `json:"status"`
	FulfillmentStatus  string          `json:"fulfillmentStatus"`
	Currency           string          `json:"currency"`
	Subtotal           int64           `json:"subtotal"`
	DiscountTotal      int64           `json:"discountTotal"`
	ShippingTotal      int64           `json:"shippingTotal"`
	TaxTotal           int64           `json:"taxTotal"`
	Total              int64           `json:"total"`
	ShipName           string          `json:"shipName"`
	ShipPhone          string          `json:"shipPhone"`
	ShipProvince       string          `json:"shipProvince"`
	ShipCity           string          `json:"shipCity"`
	ShipDistrict       string          `json:"shipDistrict"`
	ShipAddress        string          `json:"shipAddress"`
	ShipZip            string          `json:"shipZip"`
	ShipCountry        string          `json:"shipCountry"`
	PaymentMethod      string          `json:"paymentMethod"`
	PaymentMethodTitle string          `json:"paymentMethodTitle"`
	PaidAt             *utils.JSONTime `json:"paidAt"`
	CompletedAt        *utils.JSONTime `json:"completedAt"`
	Remark             string          `json:"remark"`
	CancelReason       string          `json:"cancelReason"`
	CreateTime         utils.JSONTime  `json:"createTime"`
}

type VisitorOrderItemResp struct {
	ID           uint64 `json:"id"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	UnitPrice    int64  `json:"unitPrice"`
	Quantity     int    `json:"quantity"`
	LineTotal    int64  `json:"lineTotal"`
}

type VisitorOrderLogResp struct {
	FromStatus string         `json:"fromStatus"`
	ToStatus   string         `json:"toStatus"`
	CreateTime utils.JSONTime `json:"createTime"`
}

type VisitorOrderDetailResp struct {
	Head  *VisitorOrderResp       `json:"head"`
	Items []*VisitorOrderItemResp `json:"items"`
	Logs  []*VisitorOrderLogResp  `json:"logs"`
}

type VisitorOrderListResp struct {
	List  []*VisitorOrderResp `json:"list"`
	Total int64               `json:"total"`
}

type VisitorReturnResp struct {
	ID           uint64                   `json:"id"`
	OrderID      uint64                   `json:"orderId"`
	OrderNo      string                   `json:"orderNo"`
	ReturnNo     string                   `json:"returnNo"`
	Status       string                   `json:"status"`
	Reason       string                   `json:"reason"`
	RefundAmount int64                    `json:"refundAmount"`
	Items        []*VisitorReturnItemResp `json:"items"`
	ReviewedAt   *utils.JSONTime          `json:"reviewedAt"`
	ReceivedAt   *utils.JSONTime          `json:"receivedAt"`
	RefundedAt   *utils.JSONTime          `json:"refundedAt"`
	CreateTime   utils.JSONTime           `json:"createTime"`
}

type VisitorReturnItemResp struct {
	OrderItemID      uint64 `json:"orderItemId"`
	ProductName      string `json:"productName"`
	VariantLabel     string `json:"variantLabel"`
	SKU              string `json:"sku"`
	Quantity         int    `json:"quantity"`
	ReceivedQuantity int    `json:"receivedQuantity"`
	UnitPrice        int64  `json:"unitPrice"`
	RefundAmount     int64  `json:"refundAmount"`
}

type VisitorReturnListResp struct {
	List  []*VisitorReturnResp `json:"list"`
	Total int64                `json:"total"`
}
