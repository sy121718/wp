package orderdto

import "strings"

func NormalizeCountryCode(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) != 2 {
		return ""
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return ""
		}
	}
	return strings.ToUpper(v)
}

type OrderItemReq struct {
	VariantID string `json:"variantId" form:"variantId"`
	Quantity  int    `json:"quantity" form:"quantity"`

	WarehouseID string `json:"warehouseId" form:"warehouseId"`
}

type OrderAddress struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Address  string `json:"address"`
	Zip      string `json:"zip"`

	Country string `json:"country"`
}

type CreateOrderReq struct {
	ProjectID          string         `json:"projectId" form:"projectId"`
	CustomerEmail      string         `json:"customerEmail" form:"customerEmail"`
	CustomerName       string         `json:"customerName" form:"customerName"`
	CustomerPhone      string         `json:"customerPhone" form:"customerPhone"`
	Items              []OrderItemReq `json:"items" form:"items"`
	Shipping           OrderAddress   `json:"shipping" form:"shipping"`
	Billing            OrderAddress   `json:"billing" form:"billing"`
	PaymentMethod      string         `json:"paymentMethod" form:"paymentMethod"`
	PaymentMethodTitle string         `json:"paymentMethodTitle" form:"paymentMethodTitle"`

	Currency string `json:"currency" form:"currency"`

	ShippingTotal int64 `json:"shippingTotal" form:"shippingTotal"`
	DiscountTotal int64 `json:"discountTotal" form:"discountTotal"`

	CouponCode string `json:"couponCode" form:"couponCode"`
	Remark     string `json:"remark" form:"remark"`

	RequestID string `json:"requestId" form:"requestId"`

	AdminNote string `json:"adminNote" form:"adminNote"`

	Locale string `json:"locale" form:"locale"`

	ProvisionGuestAccount *bool `json:"provisionGuestAccount" form:"provisionGuestAccount"`

	Attribution *Attribution `json:"attribution" form:"attribution"`

	UserID    *uint64 `json:"-"`
	IPAddress string  `json:"-"`
	UserAgent string  `json:"-"`
	CreateBy  uint64  `json:"-"`
}

type ListOrderReq struct {
	ProjectID     string `form:"projectId"`
	Status        string `form:"status"`
	Keyword       string `form:"keyword"`
	PaymentMethod string `form:"paymentMethod"`

	CreatedVia        string  `form:"createdVia"`
	FulfillmentStatus string  `form:"fulfillmentStatus"`
	Offset            int     `form:"offset"`
	Limit             int     `form:"limit"`
	UserID            *uint64 `form:"-"`
}

type GetOrderReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
}

type ChangeStatusReq struct {
	OrderID  uint64 `json:"orderId" form:"orderId"`
	ToStatus string `json:"toStatus" form:"toStatus"`
	Remark   string `json:"remark" form:"remark"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

type CancelOrderReq struct {
	OrderID uint64 `json:"orderId" form:"orderId"`
	Reason  string `json:"reason" form:"reason"`

	ProjectID string `json:"-"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

type PayOrderReq struct {
	OrderID uint64 `json:"orderId" form:"orderId"`

	PaymentMethod      string `json:"paymentMethod" form:"paymentMethod"`
	PaymentMethodTitle string `json:"paymentMethodTitle" form:"paymentMethodTitle"`
	TransactionID      string `json:"transactionId" form:"transactionId"`
	Remark             string `json:"remark" form:"remark"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

type RefundOrderReq struct {
	OrderID       uint64 `json:"orderId" form:"orderId"`
	Reason        string `json:"reason" form:"reason"`
	TransactionID string `json:"transactionId" form:"transactionId"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

type UpdateOrderNoteReq struct {
	OrderID   uint64 `json:"orderId" form:"orderId"`
	AdminNote string `json:"adminNote" form:"adminNote"`

	OperatorType string `json:"-" form:"-"`
	OperatorID   uint64 `json:"-" form:"-"`
	OperatorName string `json:"-" form:"-"`
}

type GetOrderByNoReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	OrderNo   string `form:"orderNo" json:"orderNo"`
}

type CouponSaveReq struct {
	ID  uint64   `form:"id" json:"id"`
	IDs []string `form:"ids" json:"ids"`

	ProjectID string `form:"projectId" json:"projectId"`

	Code string `form:"code" json:"code"`
	Name string `form:"name" json:"name"`

	DiscountType  string `form:"discountType" json:"discountType"`
	DiscountValue int64  `form:"discountValue" json:"discountValue"`

	MinSubtotal int64 `form:"minSubtotal" json:"minSubtotal"`

	MaxUses int `form:"maxUses" json:"maxUses"`

	PerUserLimit int `form:"perUserLimit" json:"perUserLimit"`

	StartsAt string `form:"startsAt" json:"startsAt"`
	EndsAt   string `form:"endsAt" json:"endsAt"`

	Status int    `form:"status" json:"status"`
	Remark string `form:"remark" json:"remark"`

	OperatorID   uint64 `form:"-" json:"-"`
	OperatorName string `form:"-" json:"-"`
}

type CouponListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Keyword   string `form:"keyword" json:"keyword"`

	Status string `form:"status" json:"status"`
	Offset int    `form:"offset" json:"offset"`
	Limit  int    `form:"limit" json:"limit"`
}

type CouponValidateReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	Code      string `form:"code" json:"code"`

	Subtotal int64 `form:"subtotal" json:"subtotal"`

	UserID uint64 `form:"-" json:"-"`
}

type CouponRedemptionListReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	CouponID  uint64 `form:"couponId" json:"couponId"`
	Code      string `form:"code" json:"code"`
	OrderID   uint64 `form:"orderId" json:"orderId"`
	Offset    int    `form:"offset" json:"offset"`
	Limit     int    `form:"limit" json:"limit"`
}

type FindOrderReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	Keyword string `json:"keyword" form:"keyword"`
	Status  string `json:"status" form:"status"`

	From  string `json:"from" form:"from"`
	To    string `json:"to" form:"to"`
	Limit int    `json:"limit" form:"limit"`
}

type FindOrderResp struct {
	List  []*OrderResp `json:"list"`
	Total int64        `json:"total"`

	From string `json:"from"`
	To   string `json:"to"`
}
