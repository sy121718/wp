package productdto

import productenums "go_wp/internal/module/product/enums"

type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids"`

	ProjectID string `json:"projectId" form:"projectId"`
}

type BulkPricingJSONReq struct {
	IDs        []string `json:"ids" form:"ids"`
	ProjectID  string   `json:"projectId" form:"projectId"`
	Note       string   `json:"note" form:"note"`
	RuleType   string   `json:"ruleType" form:"ruleType"`
	Multiplier string   `json:"multiplier" form:"multiplier"`
	Amount     string   `json:"amount" form:"amount"`
	Margin     string   `json:"margin" form:"margin"`
	Rounding   string   `json:"rounding" form:"rounding"`
}

func (r *BulkPricingJSONReq) ServiceReq(operatorID string) *BulkPricingReq {
	return &BulkPricingReq{
		IDs:        r.IDs,
		ProjectID:  r.ProjectID,
		Note:       r.Note,
		OperatorID: operatorID,
		Rule: PricingRuleReq{
			ProjectID:  r.ProjectID,
			RuleType:   r.RuleType,
			Rounding:   r.Rounding,
			Scope:      productenums.PricingScopeProduct,
			RuleParams: PricingParams(r.RuleType, r.Multiplier, r.Amount, r.Margin),
		},
	}
}

type BulkPricingReq struct {
	IDs []string

	ProjectID string

	Note string

	OperatorID string

	Rule PricingRuleReq
}
