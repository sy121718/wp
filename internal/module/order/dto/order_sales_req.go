package orderdto

type SalesOverviewReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`

	Status string `form:"status" json:"status"`

	Monthly int `form:"monthly" json:"monthly"`
}
