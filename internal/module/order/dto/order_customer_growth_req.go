package orderdto

// CustomerGrowthReq 按「工程 + 时间区间」取客户增长事实。
//
// 时间字段约定与 OrderRangeSummaryReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerGrowthReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
}
