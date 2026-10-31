package cartdto

type CartItem struct {
	VariantID    string `json:"variantId"`
	ProductID    string `json:"productId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	UnitPrice    int64  `json:"unitPrice"`
	Quantity     int    `json:"quantity"`
	LineTotal    int64  `json:"lineTotal"`

	Available   *int `json:"available"`
	Missing     bool `json:"missing"`
	MaxQuantity int  `json:"maxQuantity"`
}

type CartSnapshot struct {
	Items     []*CartItem `json:"items"`
	LineCount int         `json:"lineCount"`
	ItemCount int         `json:"itemCount"`
	Total     int64       `json:"total"`
	Currency  string      `json:"currency"`
	Empty     bool        `json:"empty"`
	Cookie    string      `json:"-"`
}

type CheckoutResp struct {
	OrderID  uint64 `json:"orderId"`
	OrderNo  string `json:"orderNo"`
	Status   string `json:"status"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
	Paid     bool   `json:"paid"`

	PaymentError  string `json:"paymentError,omitempty"`
	Email         string `json:"email"`
	AccountMailed bool   `json:"accountMailed"`
	Cookie        string `json:"-"`
}

type PaymentCallbackResp struct {
	OrderID uint64
	OrderNo string
	Status  string
	Paid    bool
	Already bool
	Applied bool

	Message string
}
