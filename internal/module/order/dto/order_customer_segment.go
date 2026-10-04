package orderdto

// order_customer_segment.go — 按客户分段取 user_id 列表（客户列表筛选用）。
//
// 与 CustomerGrowthReq/Resp 的分工：那个回答「这段时间有多少新客 / 回头客 / 复购」
//（概览页要的数字），这个回答「**是哪些人**」（列表页要的行）。两者共用同一段 SQL
//（orderCustomerCTEs），所以「概览说 12、列表筛出 13」这种自相矛盾不会有地方长出来。

// CustomerSegmentIDsReq 按「工程 + 区间 + 分段」取客户 id。
//
// 时间字段约定与 CustomerGrowthReq 一致（YYYY-MM-DD、含当天、两者必填、解析只在 service）。
type CustomerSegmentIDsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
	// Segment 取 "new" / "returning" / "repurchasing"（白名单，见 model 的 CustomerSegment）。
	Segment string `form:"segment" json:"segment"`
	Limit   int    `form:"limit" json:"limit"`
	Offset  int    `form:"offset" json:"offset"`
}

// CustomerSegmentIDsResp 分段内的客户 id 与总数。
//
// Total 是**过滤后的全量**（不受 Limit 影响）：列表页要显示「共 N 人」，
// 而那个 N 必须与概览页同一分段的计数相等 —— 对账闸门就是拿这两个数比。
type CustomerSegmentIDsResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	Segment   string `json:"segment"`
	// UserIDs 本页的客户 id（按 id 升序，稳定分页）。
	UserIDs []int64 `json:"userIds"`
	// Total 分段内的客户总数。
	Total int64 `json:"total"`
}
