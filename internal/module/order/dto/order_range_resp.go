package orderdto

// order_range_resp.go — 「区间订单摘要」的只读响应（概览页 KPI 与只读聚合共用）。

// OrderRangeSummaryResp 区间内的订单事实。
//
// 金额一律整数分 + 展示用 Label：分与元的换算只在 service 发生一次，
// 页面与模板都不做算术（同 CustomerOrderSummaryResp 的口径 —— 两处换算迟早分叉）。
type OrderRangeSummaryResp struct {
	ProjectID string `json:"projectId"`
	// From / To **实际生效**的窗口（YYYY-MM-DD，均为含当天）。
	//
	// 回显生效值而不是回显请求值：请求可能给了未来日期（会被收敛到今天），
	// 页面上的标题要写「哪一段」，写请求值就是一句错的说明。
	From string `json:"from"`
	To   string `json:"to"`
	// OrderCount 区间内创建的全部订单（含取消与退款）：回答「这段时间下了几单」。
	//
	// 与 NetSales 不是同一批单：销售额只算钱进来的那些。两个数字放在一起时，
	// 「12 单 / 800 元」是合理的（有几单取消了），不是对不上。
	OrderCount int64 `json:"orderCount"`
	// PaidOrderCount 计入消费口径的订单数（已付款 / 已发货 / 已完成）。
	PaidOrderCount int64 `json:"paidOrderCount"`
	// NetSales 净销售额（分）：计入消费的订单金额 − 已实际收货的退款额。
	NetSales      int64  `json:"netSales"`
	NetSalesLabel string `json:"netSalesLabel"`
}
