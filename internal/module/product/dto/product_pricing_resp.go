package productdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

type PricingRuleTypeResp struct {
	Type string `json:"type"`

	RequiresCost bool `json:"requiresCost"`
}

type PricingRoundingOptionResp struct {
	Value string `json:"value"`
}

type PricingLineResp struct {
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	ProductSlug string `json:"productSlug"`
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`

	CostPrice *float64 `json:"costPrice"`
	OldPrice  float64  `json:"oldPrice"`

	NewPrice float64 `json:"newPrice"`

	Diff float64 `json:"diff"`

	Status string `json:"status"`

	Reason string `json:"reason"`
}

type PricingPreviewResp struct {
	RuleType   string          `json:"ruleType"`
	RuleParams json.RawMessage `json:"ruleParams"`

	RuleValues RuleValuesResp `json:"ruleValues"`
	Rounding   string         `json:"rounding"`
	Scope      string         `json:"scope"`
	ProjectID  string         `json:"projectId"`

	TargetCount    int `json:"targetCount"`
	ChangedCount   int `json:"changedCount"`
	UnchangedCount int `json:"unchangedCount"`
	SkippedCount   int `json:"skippedCount"`

	Lines []*PricingLineResp `json:"lines"`
}

type PricingApplyResp struct {
	AdjustmentID string          `json:"adjustmentId"`
	RuleType     string          `json:"ruleType"`
	RuleParams   json.RawMessage `json:"ruleParams"`

	RuleValues   RuleValuesResp `json:"ruleValues"`
	Rounding     string         `json:"rounding"`
	Scope        string         `json:"scope"`
	ProjectID    string         `json:"projectId"`
	TargetCount  int            `json:"targetCount"`
	ChangedCount int            `json:"changedCount"`

	RecalculatedTags int `json:"recalculatedTags"`

	Lines []*PricingLineResp `json:"lines"`
}

type PricingFilterPartResp struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PriceAdjustmentItemResp struct {
	ID          string  `json:"id"`
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	VariantID   string  `json:"variantId"`
	SKUCode     string  `json:"skuCode"`
	OldPrice    float64 `json:"oldPrice"`
	NewPrice    float64 `json:"newPrice"`

	Diff      float64        `json:"diff"`
	CreatedAt utils.JSONTime `json:"createdAt"`
}

type PriceAdjustmentResp struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"projectId"`
	RuleType   string          `json:"ruleType"`
	RuleParams json.RawMessage `json:"ruleParams"`

	RuleValues RuleValuesResp  `json:"ruleValues"`
	Rounding   string          `json:"rounding"`
	Scope      string          `json:"scope"`
	TargetID   string          `json:"targetId"`
	Filter     json.RawMessage `json:"filter"`

	FilterParts  []PricingFilterPartResp `json:"filterParts"`
	VariantCount int                     `json:"variantCount"`
	ChangedCount int                     `json:"changedCount"`
	Note         string                  `json:"note"`
	OperatorID   string                  `json:"operatorId"`
	CreatedAt    utils.JSONTime          `json:"createdAt"`

	Items []*PriceAdjustmentItemResp `json:"items,omitempty"`
}
