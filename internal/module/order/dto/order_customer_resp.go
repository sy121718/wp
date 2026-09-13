package orderdto

// order_customer_resp.go — 后台「按客户看订单」的只读响应（客户管理页用）。

import "time"

// CustomerOrderSummaryResp 一个客户在某个工程下的订单聚合。
//
// 金额一律整数分，同时给展示用 Label：分与元的换算只在 service 发生一次，
// 页面与模板都不做算术（同 OrderResp 的口径 —— 两处换算迟早分叉）。
type CustomerOrderSummaryResp struct {
	UserID    uint64 `json:"userId"`
	ProjectID string `json:"projectId"`
	// OrderCount 该客户在该工程下的全部订单数（含取消与退款单：它回答的是「下过几单」）。
	OrderCount int64 `json:"orderCount"`
	// PaidOrderCount 计入消费口径的订单数（已付款 / 已发货 / 已完成）。
	PaidOrderCount int64 `json:"paidOrderCount"`
	// TotalAmount 累计消费（分）。
	//
	// 口径钉在这里而不是留给调用方各自判断：取消单与退款单**不算消费**
	//（钱没进来、或者已经退回去了），待付款单同样不算。把三类都算进去会得到一个
	// 「比实际流水大」的数字，而它看起来完全合理 —— 运营会拿它当业绩看。
	TotalAmount      int64  `json:"totalAmount"`
	TotalAmountLabel string `json:"totalAmountLabel"`
	// LastOrderNo / LastOrderTime 最近一单（含取消单：那是「最后一次下单」这件事实）。
	//
	// LastOrderID 是给后台页面做「点进去看那一单」的链接用的：客户页只有摘要，
	// 具体单据在订单管理页，而订单页按 id 展开详情 —— 没有这个 id，
	// 运营就得拿着单号去订单列表里搜一遍。
	LastOrderID       uint64     `json:"lastOrderId"`
	LastOrderNo       string     `json:"lastOrderNo"`
	LastOrderStatus   string     `json:"lastOrderStatus"`
	LastOrderTime     *time.Time `json:"lastOrderTime"`
	LastOrderTimeText string     `json:"lastOrderTimeText"`
	// HasOrders 显式布尔：页面据此决定「显示聚合数字」还是「显示这个客户还没下过单」，
	// 不靠零值猜（0 单 + 0 元与「查不到」在数字上无法区分）。
	HasOrders bool `json:"hasOrders"`
}
