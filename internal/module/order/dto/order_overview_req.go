package orderdto

type OrderDailySeriesReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
}

type OrderTopProductsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`

	Limit int `form:"limit" json:"limit"`
}

type OrderStatusCountsReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
}

type OrderSoldQuantityReq struct {
	ProjectID string `form:"projectId" json:"projectId"`
	From      string `form:"from" json:"from"`
	To        string `form:"to" json:"to"`
}
