package orderdto

// order_req.go — 订单模块请求（BIZ-1 销售侧）。
//
// 注意这里**没有价格字段**：订单项的单价由服务端从商品模块读取并落快照，
// 绝不接受调用方传入。客户端能传价格的接口等于把收银台交给客人自己看。
//
// 操作人（operatorId / operatorName）与来源（ipAddress / userAgent / userId）一律
// 由 inbound 覆盖写入：这些字段用 json:"-" 屏蔽，客户端传了也不生效。

// OrderItemReq 下单商品项：只传变体 id 与数量（价格与名称由服务端填）。
type OrderItemReq struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

// OrderAddress 地址（收货与账单共用同一形状）。
type OrderAddress struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Province string `json:"province"`
	City     string `json:"city"`
	District string `json:"district"`
	Address  string `json:"address"`
	Zip      string `json:"zip"`
}

// CreateOrderReq 建单请求。
type CreateOrderReq struct {
	ProjectID          string         `json:"projectId"`
	CustomerEmail      string         `json:"customerEmail"`
	CustomerName       string         `json:"customerName"`
	CustomerPhone      string         `json:"customerPhone"`
	Items              []OrderItemReq `json:"items"`
	Shipping           OrderAddress   `json:"shipping"`
	Billing            OrderAddress   `json:"billing"`
	PaymentMethod      string         `json:"paymentMethod"`
	PaymentMethodTitle string         `json:"paymentMethodTitle"`
	// ShippingTotal / DiscountTotal 由调用方给出（运费与优惠是业务策略，不属于商品域）；
	// Subtotal / Total 由服务端按商品价格算出，不接受传入。
	ShippingTotal int64  `json:"shippingTotal"`
	DiscountTotal int64  `json:"discountTotal"`
	Remark        string `json:"remark"`
	// RequestID 幂等键：同一键重复提交只落一单。
	RequestID string `json:"requestId"`
	// CreatedVia 下单入口（checkout / admin / api），空则按 checkout。
	CreatedVia string `json:"createdVia"`
	// AdminNote 后台备注：自建订单（createdVia=admin）与代发订单的填写位置。
	AdminNote string `json:"adminNote"`
	// Attribution 归因与轨迹（流量来源 / 广告参数 / 会话 / 下单前浏览轨迹）。
	// 由 inbound 从访客追踪上下文组装，允许为 nil（后台代客下单没有访客上下文）。
	Attribution *Attribution `json:"attribution"`

	// 以下由 inbound 覆盖写入，客户端不可伪造。
	UserID    *uint64 `json:"-"`
	IPAddress string  `json:"-"`
	UserAgent string  `json:"-"`
	CreateBy  uint64  `json:"-"`
}

// ListOrderReq 订单列表查询。
type ListOrderReq struct {
	ProjectID     string  `form:"projectId"`
	Status        string  `form:"status"`
	Keyword       string  `form:"keyword"`
	PaymentMethod string  `form:"paymentMethod"`
	Offset        int     `form:"offset"`
	Limit         int     `form:"limit"`
	UserID        *uint64 `form:"-"`
}

// GetOrderReq 订单详情查询。
type GetOrderReq struct {
	OrderID uint64 `form:"orderId"`
}

// ChangeStatusReq 状态流转。
type ChangeStatusReq struct {
	OrderID  uint64 `json:"orderId"`
	ToStatus string `json:"toStatus"`
	Remark   string `json:"remark"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

// CancelOrderReq 取消订单（会归还库存）。
type CancelOrderReq struct {
	OrderID uint64 `json:"orderId"`
	Reason  string `json:"reason"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}

// RefundOrderReq 退款。
type RefundOrderReq struct {
	OrderID       uint64 `json:"orderId"`
	Reason        string `json:"reason"`
	TransactionID string `json:"transactionId"`

	OperatorType string `json:"-"`
	OperatorID   uint64 `json:"-"`
	OperatorName string `json:"-"`
}
