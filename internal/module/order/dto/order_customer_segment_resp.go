package orderdto

type CustomerSegmentIDsResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	Segment   string `json:"segment"`

	UserIDs []int64 `json:"userIds"`

	Total int64 `json:"total"`

	MinOrders int `json:"minOrders"`
}
