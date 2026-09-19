package orderdto

// order_resp.go — 订单模块响应（BIZ-1 销售侧）。
//
// 金额一律以**分**为单位输出（字段名不带分/元后缀，单位由 totalLabel 类的展示字段承担）。

import (
	"go_wp/pkg/utils"
)

// 说明：金额与折扣一律带一份 Label 字段。分与元的换算只在 service 里发生一次，
// 后台页面与片段模板都不做算术 —— 两处换算迟早会分叉。

// OrderResp 订单头视图。
type OrderResp struct {
	ID                 uint64          `json:"id"`
	ProjectID          string          `json:"projectId"`
	OrderNo            string          `json:"orderNo"`
	Status             string          `json:"status"`
	UserID             *uint64         `json:"userId"`
	CustomerEmail      string          `json:"customerEmail"`
	CustomerName       string          `json:"customerName"`
	CustomerPhone      string          `json:"customerPhone"`
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
	BillName           string          `json:"billName"`
	BillPhone          string          `json:"billPhone"`
	BillProvince       string          `json:"billProvince"`
	BillCity           string          `json:"billCity"`
	BillDistrict       string          `json:"billDistrict"`
	BillAddress        string          `json:"billAddress"`
	BillZip            string          `json:"billZip"`
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

// OrderItemResp 订单项视图（快照值）。
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
	// CostPrice 行成本快照（分，来自该行归属仓的当前成本，迁移 256）。
	//
	// 对外可空：**null = 下单时该 (仓库, SKU) 尚未核算**，与 0（合法的显式成本：
	// 赠品 / 内部划拨）严格区分。毛利 = lineTotal - costPrice * quantity；
	// 汇总一律按 variantId 聚合（多口味共用成本值时各行同值）。
	CostPrice *int64 `json:"costPrice"`
}

// CancelOrderResp 取消订单结果。
//
// **空的响应体**：取消订单的全部事实都落在订单自身（状态 + cancel_reason + 状态流转流水）、
// 库存归还流水与券核销明细里，没有「结果之外还要额外上报的东西」。
//
// 曾经有一个 Warnings []string —— 那是「带警告成功」出口：先提交取消、再动库存，
// 归还失败就把警告回给前端。2026-09-19 事务收口后状态 / 流转 / 库存 / 券在同一事务里，
// 任一步失败整体回滚并返回 error，那条半截路径连同恒为空的字段一起删掉了
// （两个 handler 读它渲染提示的分支同步删除）。
//
// 本批只收敛「恒为空的那一半」：字段删掉，返回形状不动 —— 把空结构体也删掉会变成一次
// 接口签名变更（contract / service / 两个 handler 与全部调用方一起动），
// 而收益只是少一个类型。
type CancelOrderResp struct{}

// StatusLogResp 状态流转记录。
type StatusLogResp struct {
	FromStatus   string         `json:"fromStatus"`
	ToStatus     string         `json:"toStatus"`
	OperatorType string         `json:"operatorType"`
	OperatorName string         `json:"operatorName"`
	Remark       string         `json:"remark"`
	CreateTime   utils.JSONTime `json:"createTime"`
}

// CreateOrderResp 建单结果。
type CreateOrderResp struct {
	ID       uint64 `json:"id"`
	OrderNo  string `json:"orderNo"`
	Status   string `json:"status"`
	Total    int64  `json:"total"`
	Currency string `json:"currency"`
	// Duplicated 为真表示命中幂等键，返回的是既有单（不是新建的）。
	Duplicated bool `json:"duplicated"`
	// AccountMailed 为真表示这次下单顺带新建了访客账号，并把初始密码寄到了订单邮箱。
	// 前台据此提示「去邮箱收密码」—— 不提示的话，客户不知道自己已经有了账号，
	// 下次回来还会去走一遍注册。
	AccountMailed bool `json:"accountMailed"`
}

// PayOrderResp 支付落账结果。
type PayOrderResp struct {
	ID            uint64 `json:"id"`
	OrderNo       string `json:"orderNo"`
	Status        string `json:"status"`
	TransactionID string `json:"transactionId"`
	// AlreadyPaid 为真表示这一单此前已经付过：本次调用**没有改动任何列**，
	// 返回的是当时的结论。网关重发通知、访客连点两次下单都会命中这里。
	AlreadyPaid bool `json:"alreadyPaid"`
	// NeedsManualReview 为真表示支付成功但订单已取消/已退款，只记流水不改状态，待人工核对。
	NeedsManualReview bool `json:"needsManualReview"`
}

// OrderDetailResp 详情（头 + 项 + 流转链）。
type OrderDetailResp struct {
	Head  *OrderResp       `json:"head"`
	Items []*OrderItemResp `json:"items"`
	Logs  []*StatusLogResp `json:"logs"`
}

// OrderListResp 列表结果。
type OrderListResp struct {
	List   []*OrderResp     `json:"list"`
	Total  int64            `json:"total"`
	Counts map[string]int64 `json:"counts"`
}

// VisitorOrderListResp 访客自己的订单列表。
//
// 刻意不带 Counts：订单管理页的「各状态各有多少单」是全站口径，
// 给访客看等于把别人的单量也一并告诉他。
type VisitorOrderListResp struct {
	List  []*OrderResp `json:"list"`
	Total int64        `json:"total"`
}

// CouponResp 优惠码视图。
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
	// TimeZone 时间窗的解释口径（审计 TX-011）。
	//
	// 与 startsAt / endsAt 一起返回，后台表单据此提示「填的是哪个时区的时间」——
	// 少了它，界面上只有一个裸时间，运营只能靠猜；而猜错的后果是券提前生效或永远不生效。
	TimeZone    string         `json:"timeZone"`
	Status      int            `json:"status"`
	StatusLabel string         `json:"statusLabel"`
	Remark      string         `json:"remark"`
	CreateTime  utils.JSONTime `json:"createTime"`
	UpdateTime  utils.JSONTime `json:"updateTime"`
}

// CouponListResp 列表结果。
type CouponListResp struct {
	List  []*CouponResp `json:"list"`
	Total int64         `json:"total"`
}

// CouponValidateResp 试算结论。
type CouponValidateResp struct {
	CouponID       uint64 `json:"couponId"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	DiscountType   string `json:"discountType"`
	DiscountValue  int64  `json:"discountValue"`
	DiscountAmount int64  `json:"discountAmount"`
	DiscountLabel  string `json:"discountLabel"`
	Subtotal       int64  `json:"subtotal"`
	// Total 折后应付（不含运费与税：运费是另一件事，不能混进优惠口径）。
	Total      int64  `json:"total"`
	TotalLabel string `json:"totalLabel"`
	// Message 面向访客的结论文案（可用，或为什么不可用）。
	Message string `json:"message"`
	Usable  bool   `json:"usable"`
}

// CouponRedemptionResp 核销记录（一行 = 一次核销）。
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

// CouponRedemptionListResp 核销记录列表。
type CouponRedemptionListResp struct {
	List  []*CouponRedemptionResp `json:"list"`
	Total int64                   `json:"total"`
}
