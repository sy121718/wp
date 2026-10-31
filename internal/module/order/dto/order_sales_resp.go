package orderdto

type SalesOverviewResp struct {
	ProjectID string `json:"projectId"`

	From string `json:"from"`
	To   string `json:"to"`

	Status string `json:"status"`

	Monthly int `json:"monthly"`

	Currency string `json:"currency"`

	OrderCount int64 `json:"orderCount"`

	ItemRows int64 `json:"itemRows"`

	Units int64 `json:"units"`

	Sales int64 `json:"sales"`

	AvgOrderValue int64 `json:"avgOrderValue"`

	AvgCustomerValue int64 `json:"avgCustomerValue"`

	AvgItemsPerOrder float64 `json:"avgItemsPerOrder"`

	AvgSalesPerUnit int64 `json:"avgSalesPerUnit"`

	GrossProfit int64 `json:"grossProfit"`

	CostIncomplete bool `json:"costIncomplete"`

	CostIncompleteRows int64 `json:"costIncompleteRows"`

	Customers int64 `json:"customers"`

	NewCustomers int64 `json:"newCustomers"`

	ReturningCustomers int64 `json:"returningCustomers"`

	Repurchasers int64 `json:"repurchasers"`

	RepurchaseRatePct float64 `json:"repurchaseRatePct"`

	Compare *SalesCompareDTO `json:"compare,omitempty"`

	Converted *ConvertedOverview `json:"converted,omitempty"`

	MonthlyPoints []SalesMonthlyPointDTO `json:"monthlyPoints"`
}

type SalesMonthlyPointDTO struct {
	Month string `json:"month"`

	OrderCount int64 `json:"orderCount"`

	Sales int64 `json:"sales"`

	Customers int64 `json:"customers"`

	NewOrderCount int64 `json:"newOrderCount"`

	ReturningOrderCount int64 `json:"returningOrderCount"`

	GuestOrderCount int64 `json:"guestOrderCount"`

	NewSales int64 `json:"newSales"`

	ReturningSales int64 `json:"returningSales"`

	GuestSales int64 `json:"guestSales"`

	NewCustomers int64 `json:"newCustomers"`

	ReturningCustomers int64 `json:"returningCustomers"`
}

type SalesCompareDTO struct {
	From string `json:"from"`
	To   string `json:"to"`

	OrderCount int64 `json:"orderCount"`
	Sales      int64 `json:"sales"`
	Customers  int64 `json:"customers"`

	OrderCountChangePct *float64 `json:"orderCountChangePct,omitempty"`

	SalesChangePct *float64 `json:"salesChangePct,omitempty"`

	CustomersChangePct *float64 `json:"customersChangePct,omitempty"`
}
