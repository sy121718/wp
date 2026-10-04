package orderdto

// order_overview_req.go — 概览页三块只读聚合的请求（按天趋势 / 热销榜 / 状态计数）。
//
// 时间字段的约定与 OrderRangeSummaryReq 完全一致（字符串 YYYY-MM-DD、含当天、两者必填、
// 解析只在 service），这里不重复那套理由 —— 三处只要有一处改成「留空用默认窗口」，
// 页面上就会出现一段说不清是哪天的数字。

// OrderDailySeriesReq 按「工程 + 时间区间」取订单的按天趋势。
type OrderDailySeriesReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
}

// OrderTopProductsReq 按「工程 + 时间区间」取热销商品榜。
type OrderTopProductsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Limit <= 0 时用默认条数（页面看 5 条）；超过 model.MaxTopProductLimit 时截到上限。
	Limit int `form:"limit" json:"limit"`
}

// OrderStatusCountsReq 取各状态的订单条数（概览页「待处理」卡）。
//
// 没有时间字段：这一块回答的是「**现在**有多少单等着处理」，加时间窗会变成
// 「这段时间里出现过多少待处理单」—— 那是另一个问题，且会随区间变长而膨胀。
type OrderStatusCountsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
}
