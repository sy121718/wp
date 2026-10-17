package orderdto

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
	// MinOrders 实际生效的复购次数门槛（0 = 该分段不看次数）。
	//
	// 回显它是为了让调用方能核对「我筛的是不是我以为的那一档」——
	// 页面把筛选条件回显给用户时用的就是这个值，而不是 URL 参数。
	MinOrders int `json:"minOrders"`
}
