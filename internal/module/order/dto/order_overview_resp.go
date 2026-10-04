package orderdto

// order_overview_resp.go — 概览页三块只读聚合的响应（按天趋势 / 热销榜 / 状态计数）。
//
// 三块都只回**结论**：金额整数分 + 展示用 Label，状态计数回已经解释过的口径字段。
// 页面与 AI 都不做算术，也不解释状态名（解释散在渲染层就会出现「页面算一套、AI 说一套」）。

// OrderDailyPointDTO 某一天的订单事实。
type OrderDailyPointDTO struct {
	// Day YYYY-MM-DD，**UTC 日桶**（与访问统计的按天口径一致）。
	Day string `json:"day"`
	// OrderCount 当天创建的全部订单（含取消与退款）。
	OrderCount int64 `json:"orderCount"`
	// PaidOrderCount 当天创建、计入消费口径的订单数。
	PaidOrderCount int64 `json:"paidOrderCount"`
	// NetSales 当天净销售额（分）。
	NetSales      int64  `json:"netSales"`
	NetSalesLabel string `json:"netSalesLabel"`
}

// OrderDailySeriesResp 一段区间内**逐日连续**的订单数据。
//
// Points 是补过零的完整序列（区间内每天一个点，没有订单的那天为 0）：
// 由 service 补齐而不是页面补 —— 页面补零要自己再算一遍「区间里有哪几天」，
// 那正是时区与边界最容易分叉的一步。
type OrderDailySeriesResp struct {
	ProjectID string `json:"projectId"`
	// From / To 实际生效的窗口（YYYY-MM-DD，均为含当天，与请求值可能不同）。
	From string `json:"from"`
	To   string `json:"to"`
	// Points 区间内每天一个点，按日期升序；长度 = 区间天数（含首尾）。
	Points []OrderDailyPointDTO `json:"points"`
}

// OrderTopProductItemDTO 榜单里的一行。
type OrderTopProductItemDTO struct {
	// Rank 从 1 起的名次（页面直接渲染，不靠渲染层数序号 —— 那份序号与排序规则是两处事实）。
	Rank        int    `json:"rank"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`
	// Quantity 区间内该商品被买走的总件数。
	Quantity int64 `json:"quantity"`
	// Amount 该商品的行实付合计（分，**不含退款分摊**，见 model 的说明）。
	Amount      int64  `json:"amount"`
	AmountLabel string `json:"amountLabel"`
}

// OrderTopProductsResp 热销商品榜（已按销量降序，名次已写入每一行）。
type OrderTopProductsResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	// Limit 实际生效的条数上限（回显生效值：请求 1000 会被截到 MaxTopProductLimit）。
	Limit int                      `json:"limit"`
	Items []OrderTopProductItemDTO `json:"items"`
}

// OrderStatusCountsResp 各状态订单条数与几个已解释过的口径。
//
// Counts 是**原始状态 → 条数**（只含出现过的状态，缺失即 0）；下面三个字段是页面与 AI
// 真正要用的口径，避免消费方各自去猜「待处理」该等于哪几个状态之和。
type OrderStatusCountsResp struct {
	ProjectID string `json:"projectId"`
	// Counts 状态名 → 条数（状态名见 model 的状态常量）。
	Counts map[string]int64 `json:"counts"`
	// PendingCount 待付款。
	PendingCount int64 `json:"pendingCount"`
	// ShipPendingCount 待发货（已付款但还没发出）。
	ShipPendingCount int64 `json:"shipPendingCount"`
	// TotalCount 全部状态的订单总数。
	TotalCount int64 `json:"totalCount"`
}
