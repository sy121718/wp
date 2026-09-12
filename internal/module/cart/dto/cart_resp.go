package cartdto

// cart_resp.go — 购物车模块响应。
//
// 金额一律以**分**为单位（与订单域一致）；同时给出 ¥ 前缀的展示串 —— 拼展示串这种事
// 只该有一处实现，散在模板里就会出现「有的地方两位小数、有的地方没有」。

// CartItem 购物车中的一行：商品事实 + 数量 + 行小计 + 可用量。
type CartItem struct {
	VariantID    string
	ProductID    string
	ProductName  string
	VariantLabel string
	SKU          string

	UnitPrice      int64
	Quantity       int
	LineTotal      int64
	UnitPriceLabel string
	LineTotalLabel string

	// Available 可用量；AvailableKnown 为假表示查不到（库存端口未接入 / 变体已删）。
	// 「未知」与「为 0」必须分开：把未知说成缺货，会让一次库存抖动变成前台整店下架。
	Available      int
	AvailableKnown bool
	InStock        bool
	AvailableText  string

	// Missing 为真表示这个变体已经查不到（下架 / 删除 / 跨工程）：
	// 展示成「商品已下架」，且**不计入小计** —— 但它留在购物车里让访客自己删，
	// 静默移掉一件他发现少了东西却不知道为什么的商品更糟。
	Missing bool

	// MaxQuantity 本行数量输入框的上限（可用量已知时按可用量封顶，否则给硬上限）。
	MaxQuantity int
	// Removable 是否允许在该行操作（当前恒为真，保留字段是为了片段模板与商品卡字段槽位同形）。
	Removable bool
}

// CartSnapshot 购物车快照。
//
// Cookie 只在**变更类**方法（Add / SetQuantity / Clear）返回时非空，表示
// 「应写回响应的新 cookie 值」；只读查询（View）留空 —— 双态是有意的：
// 调用方据此区分「要不要动响应头」，而不是把只读查询也变成一次写 cookie。
type CartSnapshot struct {
	Items      []*CartItem
	LineCount  int
	ItemCount  int
	Total      int64
	TotalLabel string
	Currency   string
	Empty      bool
	Cookie     string
}

// CheckoutResp 结算结果。
//
// Paid 为假时**不是错误**：订单已经建好了，只是钱没收到（支付通道失败）。
// 单号照样返回给访客 —— 否则一次扣款失败会让客户连自己下过单都不知道。
type CheckoutResp struct {
	OrderID    uint64
	OrderNo    string
	Status     string
	Total      int64
	TotalLabel string
	Currency   string
	Paid       bool
	// PaymentError 支付未成功时的原因（面向访客的文案）。
	PaymentError string
	// Email 订单邮件的收件地址。
	Email string
	// AccountMailed 是否为这次下单新建了账号并寄出初始密码。
	AccountMailed bool
	// Cookie 结算成功后应写回的（空）购物车 cookie 值。
	Cookie string
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
