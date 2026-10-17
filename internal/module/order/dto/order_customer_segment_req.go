package orderdto

// CustomerSegmentIDsReq 按「工程 + 区间 + 分段」取客户 id。
//
// 时间字段约定与 CustomerGrowthReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerSegmentIDsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Segment 取 "new" / "returning" / "repurchasing"（白名单，见 model 的 CustomerSegment）。
	Segment string `form:"segment" json:"segment"`
	// MinOrders 复购次数下限（只对 segment = repurchasing 有意义；0 = 用默认门槛）。
	//
	// 它让「复购 ≥ 3 次」这种筛选与「复购」走同一段代码：单独开一个 segment 名
	// 会让「什么是复购」在代码里出现第二个定义。
	MinOrders int `form:"minOrders" json:"minOrders"`
	Limit     int `form:"limit" json:"limit"`
	Offset    int `form:"offset" json:"offset"`
}
