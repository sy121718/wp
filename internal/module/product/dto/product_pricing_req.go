package productdto

import (
	productenums "go_wp/internal/module/product/enums"
	json "go_wp/pkg/jsonx"
	"net/url"
)

type PricingRuleReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	RuleType string `json:"ruleType" form:"ruleType"`

	RuleParams json.RawMessage `json:"ruleParams" form:"-"`

	Multiplier string `json:"-" form:"multiplier"`
	Amount     string `json:"-" form:"amount"`
	Margin     string `json:"-" form:"margin"`

	Rounding string `json:"rounding" form:"rounding"`

	Scope string `json:"scope" form:"scope"`

	TargetID string `json:"targetId" form:"targetId"`

	Status     string `json:"status" form:"status"`
	Keyword    string `json:"keyword" form:"keyword"`
	CategoryID string `json:"categoryId" form:"categoryId"`
	BrandID    string `json:"brandId" form:"brandId"`
	TagID      string `json:"tagId" form:"tagId"`
}

// PricingPreviewReq 与 ApplyPricing 共用同一张表单：模板里的「备注」字段在预览时
// 也要能绑定（不落库，只是同一表单的两种提交按钮），所以两个 DTO 的字段集必须一致。
type PricingPreviewReq struct {
	PricingRuleReq

	Note string `json:"note" form:"note"`
}

type PricingApplyReq struct {
	PricingRuleReq

	Note string `json:"note" form:"note"`

	OperatorID string `json:"-" form:"-"`
}

type ListPriceAdjustmentReq struct {
	ProjectID string `form:"projectId"`
	Limit     int    `form:"limit"`

	IncludeItems bool `form:"includeItems"`
}

type GetPriceAdjustmentReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

func (r *PricingRuleReq) BindForm(vals url.Values) error {
	if vals.Has("ruleParams") {
		return bindRawJSONField(vals, "ruleParams", &r.RuleParams)
	}
	r.RuleParams = PricingParams(r.RuleType, r.Multiplier, r.Amount, r.Margin)
	return nil
}

func PricingParams(ruleType, multiplier, amount, margin string) json.RawMessage {
	params := map[string]any{}
	switch ruleType {
	case productenums.PricingRuleCostMultiple:
		if v, err := OptFloat("multiplier", multiplier); err == nil && v != nil {
			params["multiplier"] = *v
		}
	case productenums.PricingRuleCostMarkup, productenums.PricingRuleFixedPrice:
		if v, err := OptFloat("amount", amount); err == nil && v != nil {
			params["amount"] = *v
		}
	case productenums.PricingRuleTargetMargin:
		if v, err := OptFloat("margin", margin); err == nil && v != nil {
			params["margin"] = *v
		}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return json.RawMessage("{}")
	}
	return raw
}
