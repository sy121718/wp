package orderdto

// CustomerRfmReq 按「工程 + 区间（+ 可选分段）」取 RFM 明细。
//
// 时间字段约定与 CustomerGrowthReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerRfmReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Segment 只看某一段（"vip" / "potential" / "low_value"），空为全部。
	Segment string `form:"segment" json:"segment"`
	Limit   int    `form:"limit" json:"limit"`
	Offset  int    `form:"offset" json:"offset"`
}

// CustomerRfmSegmentIDsReq 取某个 RFM 分段内的客户 id（客户列表按 RFM 分段筛用）。
//
// 与列表页那三个分段（新客 / 回头客 / 复购）**是两套不同的分段**：
// 那三个看下单行为，这三个看 RFM 总分。它们在页面上是两个独立的筛选项，
// 同时给出时取交集（见 customerPageHandle 的 rfmSegmentIDs）。
type CustomerRfmSegmentIDsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Segment "vip" / "potential" / "low_value"（白名单，认不出当场拒）。
	Segment string `form:"segment" json:"segment"`
	Limit   int    `form:"limit" json:"limit"`
	Offset  int    `form:"offset" json:"offset"`
}
