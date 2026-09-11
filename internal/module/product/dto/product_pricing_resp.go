// product_pricing_resp.go — 定价工具出参（issue #13）。
package productdto

import "encoding/json"

// PricingRuleTypeResp 内置定价规则类型（后台下拉、参数说明与规则描述的唯一定义来源）。
type PricingRuleTypeResp struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// Params 参数说明（后台直接展示，避免前端再写一份参数文档）。
	Params string `json:"params"`
	// RequiresCost 该规则是否依赖变体成本价（缺成本价的变体将被跳过）。
	RequiresCost bool `json:"requiresCost"`
}

// PricingRoundingOptionResp 尾数处理选项（后台下拉的唯一来源）。
type PricingRoundingOptionResp struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

// PricingLineResp 逐变体试算行（预览与留痕共用同一形状）。
type PricingLineResp struct {
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	ProductSlug string `json:"productSlug"`
	VariantID   string `json:"variantId"`
	SKUCode     string `json:"skuCode"`
	// CostPrice 变体当前成本价（为 nil 表示未填；成本类规则会跳过该变体）。
	CostPrice *float64 `json:"costPrice"`
	OldPrice  float64  `json:"oldPrice"`
	// NewPrice 试算出的新售价（skipped 时等于 OldPrice，作为「不改动」的明确占位）。
	NewPrice float64 `json:"newPrice"`
	// Status changed / unchanged / skipped。
	Status string `json:"status"`
	// StatusLabel 状态的中文标签（服务端翻好，模板不做第二套解释）。
	StatusLabel string `json:"statusLabel"`
	// Reason 跳过原因（enums 键；非 skipped 为空串）。
	Reason string `json:"reason"`
	// ReasonLabel 跳过原因的中文说明。
	ReasonLabel string `json:"reasonLabel"`
}

// PricingPreviewResp 试算结果（**不落库**：预览与应用共用同一份算价逻辑，
// 区别只在于预览不写价格、不写留痕）。
type PricingPreviewResp struct {
	RuleType      string          `json:"ruleType"`
	RuleParams    json.RawMessage `json:"ruleParams"`
	RuleLabel     string          `json:"ruleLabel"`
	Rounding      string          `json:"rounding"`
	RoundingLabel string          `json:"roundingLabel"`
	Scope         string          `json:"scope"`
	ScopeLabel    string          `json:"scopeLabel"`
	ProjectID     string          `json:"projectId"`
	// TargetCount 参与试算的变体数（含跳过与未改动）。
	TargetCount    int `json:"targetCount"`
	ChangedCount   int `json:"changedCount"`
	UnchangedCount int `json:"unchangedCount"`
	SkippedCount   int `json:"skippedCount"`
	// Lines 逐变体明细（顺序稳定：商品排序 → 变体排序）。
	Lines []*PricingLineResp `json:"lines"`
}

// PricingApplyResp 应用结果（落库 + 留痕）。
type PricingApplyResp struct {
	// AdjustmentID 本次调价批次 id（留痕台账主键，可据此回看逐变体改动）。
	AdjustmentID  string          `json:"adjustmentId"`
	RuleType      string          `json:"ruleType"`
	RuleParams    json.RawMessage `json:"ruleParams"`
	RuleLabel     string          `json:"ruleLabel"`
	Rounding      string          `json:"rounding"`
	RoundingLabel string          `json:"roundingLabel"`
	Scope         string          `json:"scope"`
	ScopeLabel    string          `json:"scopeLabel"`
	ProjectID     string          `json:"projectId"`
	TargetCount   int             `json:"targetCount"`
	ChangedCount  int             `json:"changedCount"`
	SkippedCount  int             `json:"skippedCount"`
	// RecalculatedTags 本次落库后顺带重算的自动标签数（变体价格变 → 价格区间/促销规则可能变归属）。
	RecalculatedTags int `json:"recalculatedTags"`
	// Lines 仅含**实际改动**的变体（未改动与被跳过的不写留痕）。
	Lines []*PricingLineResp `json:"lines"`
}

// PriceAdjustmentItemResp 调价批次逐变体留痕明细。
type PriceAdjustmentItemResp struct {
	ID          string  `json:"id"`
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	VariantID   string  `json:"variantId"`
	SKUCode     string  `json:"skuCode"`
	OldPrice    float64 `json:"oldPrice"`
	NewPrice    float64 `json:"newPrice"`
	// Diff 差值（新价 - 原价），正数为涨价。
	Diff      float64 `json:"diff"`
	CreatedAt string  `json:"createdAt"`
}

// PriceAdjustmentResp 调价批次（留痕台账，后台可回看「谁按哪条规则改了什么价」）。
type PriceAdjustmentResp struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	RuleType      string          `json:"ruleType"`
	RuleParams    json.RawMessage `json:"ruleParams"`
	RuleLabel     string          `json:"ruleLabel"`
	Rounding      string          `json:"rounding"`
	RoundingLabel string          `json:"roundingLabel"`
	Scope         string          `json:"scope"`
	ScopeLabel    string          `json:"scopeLabel"`
	TargetID      string          `json:"targetId"`
	Filter        json.RawMessage `json:"filter"`
	FilterLabel   string          `json:"filterLabel"`
	VariantCount  int             `json:"variantCount"`
	ChangedCount  int             `json:"changedCount"`
	Note          string          `json:"note"`
	OperatorID    string          `json:"operatorId"`
	CreatedAt     string          `json:"createdAt"`
	// Items 逐变体明细（仅详情接口填充；列表接口为 nil）。
	Items []*PriceAdjustmentItemResp `json:"items,omitempty"`
}
