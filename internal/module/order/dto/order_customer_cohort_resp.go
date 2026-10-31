package orderdto

type CustomerCohortCell struct {
	MonthIndex int `json:"monthIndex"`

	MonthLabel string `json:"monthLabel"`

	ActiveCustomers int64 `json:"activeCustomers"`

	RetentionRatePct float64 `json:"retentionRatePct"`

	RetentionLabel string `json:"retentionLabel"`

	Reached bool `json:"reached"`
}

type CustomerCohortRow struct {
	CohortMonth string `json:"cohortMonth"`

	CohortLabel string `json:"cohortLabel"`

	CohortSize int64 `json:"cohortSize"`

	Cells []CustomerCohortCell `json:"cells"`
}

type CustomerCohortResp struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`

	Months int `json:"months"`

	Cohorts int64 `json:"cohorts"`

	Customers int64 `json:"customers"`

	Rows []CustomerCohortRow `json:"rows"`
}
