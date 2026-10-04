package orderdto

// order_customer_growth.go — 区间客户增长（客户概览 / 概览页「新客户」KPI / AI 工具）。

// CustomerGrowthReq 按「工程 + 时间区间」取客户增长事实。
//
// 时间字段约定与 OrderRangeSummaryReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerGrowthReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
}

// CustomerGrowthResp 区间内的客户增长。
//
// 五个计数满足一组恒等式（测试钉住）：NewCustomers + ReturningCustomers == OrderingCustomers。
// 它们不是四个独立指标，而是同一批人的四种切分 —— 所以必须由同一条 SQL 一次取回。
type CustomerGrowthResp struct {
	ProjectID string `json:"projectId"`
	// From / To 实际生效的窗口（含当天，与区间摘要同口径）。
	From string `json:"from"`
	To   string `json:"to"`
	// OrderingCustomers 区间内下过消费单的客户数（复购率的分母）。
	OrderingCustomers int64 `json:"orderingCustomers"`
	// NewCustomers 首单落在区间内的客户数（不看注册时间）。
	NewCustomers int64 `json:"newCustomers"`
	// ReturningCustomers 首单在区间之前、区间内又下单的客户数。
	ReturningCustomers int64 `json:"returningCustomers"`
	// Repurchasers 区间内下了 ≥2 单的客户总数（不分新老）。
	Repurchasers int64 `json:"repurchasers"`
	// NewRepurchasers 新客里在区间内复购的（复购率分子的一半）。
	NewRepurchasers int64 `json:"newRepurchasers"`
	// RepurchaseRatePct 复购率（百分比，保留一位小数）；分母为 0 时为 0。
	RepurchaseRatePct float64 `json:"repurchaseRatePct"`
	// RepurchaseRateLabel 展示用串（"42.9%"）：分与元的换算、百分比的格式化都在
	// service 发生一次，页面与模板都不做算术。
	RepurchaseRateLabel string `json:"repurchaseRateLabel"`
}
