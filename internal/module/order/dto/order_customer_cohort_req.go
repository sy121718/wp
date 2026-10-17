package orderdto

// CustomerCohortReq 按「工程 + 分群区间」取群组留存矩阵。
//
// 时间字段约定与 CustomerGrowthReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerCohortReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Months 展示的列数上限（从首单当月算起）。0 = 用 service 的默认值。
	Months int `form:"months" json:"months"`
}
