package cartdto

// CartItem 是服务现算的商品、数量、金额和库存事实。
type CartItem struct {
	VariantID    string `json:"variantId"`
	ProductID    string `json:"productId"`
	ProductName  string `json:"productName"`
	VariantLabel string `json:"variantLabel"`
	SKU          string `json:"sku"`
	UnitPrice    int64  `json:"unitPrice"`
	Quantity     int    `json:"quantity"`
	LineTotal    int64  `json:"lineTotal"`
	// Available 为 nil 表示未知，零表示已确认缺货。
	Available   *int `json:"available"`
	Missing     bool `json:"missing"`
	MaxQuantity int  `json:"maxQuantity"`
}

// CartSnapshot 不向浏览器暴露签名 cookie 原文。
type CartSnapshot struct {
	Items     []*CartItem `json:"items"`
	LineCount int         `json:"lineCount"`
	ItemCount int         `json:"itemCount"`
	Total     int64       `json:"total"`
	Currency  string      `json:"currency"`
	Empty     bool        `json:"empty"`
	Cookie    string      `json:"-"`
}

// CheckoutResp 的 Paid=false 表示订单已创建但支付尚未成功。
type CheckoutResp struct {
	OrderID  uint64 `json:"orderId"`
	OrderNo  string `json:"orderNo"`
	Status   string `json:"status"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
	Paid     bool   `json:"paid"`
	// PaymentError 是声明处的错误 key，界面按状态展示对应文案。
	PaymentError  string `json:"paymentError,omitempty"`
	Email         string `json:"email"`
	AccountMailed bool   `json:"accountMailed"`
	Cookie        string `json:"-"`
}

// PaymentCallbackResp 回调处理结果。
//
// 三个布尔值各有各的意思，不能合并：
//   - Paid 这次回调声明的付款结果；
//   - Already 订单此前就已经是付款状态（幂等命中，本次没改任何列）；
//   - Applied 本次调用**真的改了订单状态**。
//
// 通道重发通知是常态，所以要能回答「这次我做了什么」而不是只说「收到了」。
type PaymentCallbackResp struct {
	OrderID uint64
	OrderNo string
	Status  string
	Paid    bool
	Already bool
	Applied bool
	// Message 面向运维的一句话结论（落日志 / 回给通道都够用）。
	Message string
}
