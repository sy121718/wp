package orderdto

// order_range_req.go — 「区间订单摘要」的只读请求（概览页 KPI 与只读聚合共用）。

// OrderRangeSummaryReq 按「工程 + 时间区间」取订单摘要。
//
// 时间用**字符串**而不是 time.Time：同一份结构既要服务后台原生表单（form）、
// 也要服务 JSON 接口与只读聚合，而表单里的日期天生是文本（与 CouponSaveReq 同一条理由）。
// 解析只发生在 service，非法格式在那里统一报错，而不是让两种绑定器各解出一套结果。
//
// From / To 都是 YYYY-MM-DD、**含当天**；两者都必须给：
// 概览页永远有时间范围，留空则走「静默默认窗口」—— 那是「我看到的是哪一段」
// 说不清楚的一类数字，宁可让调用方显式传。
type OrderRangeSummaryReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
}
