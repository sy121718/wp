package orderdto

type CustomerCohortReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`

	Months int `form:"months" json:"months"`
}
