package orderdto

import (
	"go_wp/pkg/utils"
)

type OrderResp struct {
	ID        uint64 `json:"id"`
	ProjectID string `json:"projectId"`
	OrderNo   string `json:"orderNo"`
	Status    string `json:"status"`

	StatusTone string `json:"-"`

	FulfillmentStatus string `json:"fulfillmentStatus"`

	FulfillmentTone string  `json:"-"`
	UserID          *uint64 `json:"userId"`
	CustomerEmail   string  `json:"customerEmail"`
	CustomerName    string  `json:"customerName"`
	CustomerPhone   string  `json:"customerPhone"`
	Currency        string  `json:"currency"`
	Subtotal        int64   `json:"subtotal"`
	DiscountTotal   int64   `json:"discountTotal"`
	ShippingTotal   int64   `json:"shippingTotal"`
	TaxTotal        int64   `json:"taxTotal"`
	Total           int64   `json:"total"`
	ShipName        string  `json:"shipName"`
	ShipPhone       string  `json:"shipPhone"`
	ShipProvince    string  `json:"shipProvince"`
	ShipCity        string  `json:"shipCity"`
	ShipDistrict    string  `json:"shipDistrict"`
	ShipAddress     string  `json:"shipAddress"`
	ShipZip         string  `json:"shipZip"`

	ShipCountry        string          `json:"shipCountry"`
	BillName           string          `json:"billName"`
	BillPhone          string          `json:"billPhone"`
	BillProvince       string          `json:"billProvince"`
	BillCity           string          `json:"billCity"`
	BillDistrict       string          `json:"billDistrict"`
	BillAddress        string          `json:"billAddress"`
	BillZip            string          `json:"billZip"`
	BillCountry        string          `json:"billCountry"`
	PaymentMethod      string          `json:"paymentMethod"`
	PaymentMethodTitle string          `json:"paymentMethodTitle"`
	TransactionID      string          `json:"transactionId"`
	PaidAt             *utils.JSONTime `json:"paidAt"`
	CompletedAt        *utils.JSONTime `json:"completedAt"`
	CreatedVia         string          `json:"createdVia"`
	IPAddress          string          `json:"ipAddress"`
	UserAgent          string          `json:"userAgent"`
	AdminNote          string          `json:"adminNote"`
	Attribution        *Attribution    `json:"attribution,omitempty"`
	Remark             string          `json:"remark"`
	CancelReason       string          `json:"cancelReason"`
	CreateTime         utils.JSONTime  `json:"createTime"`
	UpdateTime         utils.JSONTime  `json:"updateTime"`
}

type OrderItemResp struct {
	ID           uint64 `json:"id"`
	ProductID    string `json:"productId"`
	VariantID    string `json:"variantId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	UnitPrice    int64  `json:"unitPrice"`
	Quantity     int    `json:"quantity"`
	LineSubtotal int64  `json:"lineSubtotal"`
	LineDiscount int64  `json:"lineDiscount"`
	LineTax      int64  `json:"lineTax"`
	LineTotal    int64  `json:"lineTotal"`

	CostPrice *int64 `json:"costPrice"`
}

type CancelOrderResp struct{}

type StatusLogResp struct {
	FromStatus   string         `json:"fromStatus"`
	ToStatus     string         `json:"toStatus"`
	OperatorType string         `json:"operatorType"`
	OperatorName string         `json:"operatorName"`
	Remark       string         `json:"remark"`
	CreateTime   utils.JSONTime `json:"createTime"`
}

type CreateOrderResp struct {
	ID       uint64 `json:"id"`
	OrderNo  string `json:"orderNo"`
	Status   string `json:"status"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`

	Duplicated bool `json:"duplicated"`

	AccountMailed bool `json:"accountMailed"`
}

type PayOrderResp struct {
	ID            uint64 `json:"id"`
	OrderNo       string `json:"orderNo"`
	Status        string `json:"status"`
	TransactionID string `json:"transactionId"`

	AlreadyPaid bool `json:"alreadyPaid"`

	NeedsManualReview bool `json:"needsManualReview"`
}

type OrderDetailResp struct {
	Head  *OrderResp       `json:"head"`
	Items []*OrderItemResp `json:"items"`
	Logs  []*StatusLogResp `json:"logs"`
}

type OrderListResp struct {
	List  []*OrderResp `json:"list"`
	Total int64        `json:"total"`

	Counts            map[string]int64 `json:"counts"`
	FulfillmentCounts map[string]int64 `json:"fulfillmentCounts"`
}

type CouponResp struct {
	ID               uint64          `json:"id"`
	ProjectID        string          `json:"projectId"`
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	DiscountType     string          `json:"discountType"`
	DiscountValue    int64           `json:"discountValue"`
	DiscountLabel    string          `json:"discountLabel"`
	MinSubtotal      int64           `json:"minSubtotal"`
	MinSubtotalLabel string          `json:"minSubtotalLabel"`
	MaxUses          int             `json:"maxUses"`
	UsedCount        int             `json:"usedCount"`
	PerUserLimit     int             `json:"perUserLimit"`
	StartsAt         *utils.JSONTime `json:"startsAt"`
	EndsAt           *utils.JSONTime `json:"endsAt"`

	TimeZone string `json:"timeZone"`
	Status   int    `json:"status"`

	State       string `json:"state"`
	StatusLabel string `json:"statusLabel"`

	StateTone  string         `json:"-"`
	Remark     string         `json:"remark"`
	CreateTime utils.JSONTime `json:"createTime"`
	UpdateTime utils.JSONTime `json:"updateTime"`
}

type CouponListResp struct {
	List  []*CouponResp `json:"list"`
	Total int64         `json:"total"`
}

type CouponValidateResp struct {
	CouponID       uint64 `json:"couponId"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	DiscountType   string `json:"discountType"`
	DiscountValue  int64  `json:"discountValue"`
	DiscountAmount int64  `json:"discountAmount"`
	DiscountLabel  string `json:"discountLabel"`
	Subtotal       int64  `json:"subtotal"`

	Total      int64  `json:"total"`
	TotalLabel string `json:"totalLabel"`

	Message string `json:"message"`
	Usable  bool   `json:"usable"`
}

type CouponRedemptionResp struct {
	ID             uint64         `json:"id"`
	CouponID       uint64         `json:"couponId"`
	Code           string         `json:"code"`
	OrderID        uint64         `json:"orderId"`
	OrderNo        string         `json:"orderNo"`
	DiscountAmount int64          `json:"discountAmount"`
	DiscountLabel  string         `json:"discountLabel"`
	UserID         *uint64        `json:"userId"`
	CreateTime     utils.JSONTime `json:"createTime"`
}

type CouponRedemptionListResp struct {
	List  []*CouponRedemptionResp `json:"list"`
	Total int64                   `json:"total"`
}
