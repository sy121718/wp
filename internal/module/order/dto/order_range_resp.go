package orderdto

type OrderRangeSummaryResp struct {
	ProjectID string `json:"projectId"`

	From string `json:"from"`
	To   string `json:"to"`

	OrderCount int64 `json:"orderCount"`

	PaidOrderCount int64 `json:"paidOrderCount"`

	NetSales      int64  `json:"netSales"`
	NetSalesLabel string `json:"netSalesLabel"`
}
