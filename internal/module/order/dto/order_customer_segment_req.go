package orderdto

type CustomerSegmentIDsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`

	Segment string `form:"segment" json:"segment"`

	MinOrders int `form:"minOrders" json:"minOrders"`
	Limit     int `form:"limit" json:"limit"`
	Offset    int `form:"offset" json:"offset"`
}
