package orderdto

type OrderDailyPointDTO struct {
	Day string `json:"day"`

	OrderCount int64 `json:"orderCount"`

	PaidOrderCount int64 `json:"paidOrderCount"`

	NetSales      int64  `json:"netSales"`
	NetSalesLabel string `json:"netSalesLabel"`
}

type OrderDailySeriesResp struct {
	ProjectID string `json:"projectId"`

	From string `json:"from"`
	To   string `json:"to"`

	Points []OrderDailyPointDTO `json:"points"`
}

type OrderTopProductItemDTO struct {
	Rank        int    `json:"rank"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`

	Quantity int64 `json:"quantity"`

	Amount      int64  `json:"amount"`
	AmountLabel string `json:"amountLabel"`
}

type OrderTopProductsResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`

	Limit int                      `json:"limit"`
	Items []OrderTopProductItemDTO `json:"items"`
}

type OrderStatusCountsResp struct {
	ProjectID string `json:"projectId"`

	Counts map[string]int64 `json:"counts"`

	PendingCount int64 `json:"pendingCount"`

	ShipPendingCount int64 `json:"shipPendingCount"`

	TotalCount int64 `json:"totalCount"`
}

type OrderSoldQuantityResp struct {
	ProjectID string `json:"projectId"`

	From string `json:"from"`
	To   string `json:"to"`

	Quantity int64 `json:"quantity"`

	OrderCount int64 `json:"orderCount"`
}
