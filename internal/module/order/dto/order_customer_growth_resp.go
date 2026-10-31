package orderdto

type CustomerGrowthResp struct {
	ProjectID string `json:"projectId"`

	From string `json:"from"`
	To   string `json:"to"`

	OrderingCustomers int64 `json:"orderingCustomers"`

	NewCustomers int64 `json:"newCustomers"`

	ReturningCustomers int64 `json:"returningCustomers"`

	Repurchasers int64 `json:"repurchasers"`

	NewRepurchasers int64 `json:"newRepurchasers"`

	RepurchaseRatePct float64 `json:"repurchaseRatePct"`

	RepurchaseRateLabel string `json:"repurchaseRateLabel"`
}
