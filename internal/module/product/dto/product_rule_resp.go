package productdto

type RuleValuesResp struct {
	Days    int
	HasDays bool

	Value    float64
	HasValue bool

	MinPrice    float64
	HasMinPrice bool
	MaxPrice    float64
	HasMaxPrice bool
}
