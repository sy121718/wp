// product_pricing_req.go — 定价工具入参（issue #13）。
package productdto

import (
	productenums "go_wp/internal/module/product/enums"
	json "go_wp/pkg/jsonx"
	"net/url"
)

// PricingRuleReq 定价工具的规则与作用范围（试算与落库共用同一份入参）。
//
// 字段合法性一律由 service 用模块 enums 判定（未知键 / 类型不符 / 越界都给出可读原因），
// 故这里不写 binding 约束 —— 参数错误的文案只有 service 一份。
type PricingRuleReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	// RuleType 四种内置规则之一（cost_multiple / cost_markup / target_margin / fixed_price）。
	RuleType string `json:"ruleType" form:"ruleType"`
	// RuleParams 规则参数（键集合由规则类型决定；白名单外的键一律拒绝）。
	RuleParams json.RawMessage `json:"ruleParams" form:"-"`
	// 表单中的数值原文由 BindForm 归一到 RuleParams；JSON 使用 ruleParams。
	Multiplier string `json:"-" form:"multiplier"`
	Amount     string `json:"-" form:"amount"`
	Margin     string `json:"-" form:"margin"`
	// Rounding 尾数处理（none / integer / end_9 / end_99）；留空按 none。
	Rounding string `json:"rounding" form:"rounding"`
	// Scope 作用范围（sku / product / filter）。
	Scope string `json:"scope" form:"scope"`
	// TargetID 单个 SKU（变体 id）或单个商品（商品 id）范围的目标；筛选集范围忽略。
	TargetID string `json:"targetId" form:"targetId"`
	// —— 筛选集范围条件（Scope=filter 时生效；ProjectID 必填）——
	Status     string `json:"status" form:"status"`
	Keyword    string `json:"keyword" form:"keyword"`
	CategoryID string `json:"categoryId" form:"categoryId"`
	BrandID    string `json:"brandId" form:"brandId"`
	TagID      string `json:"tagId" form:"tagId"`
}

// PricingPreviewReq 应用前预览（试算，不落库）。
type PricingPreviewReq struct {
	PricingRuleReq
}

// PricingApplyReq 应用调价（算价 → 写回 product_variants.price → 留痕）。
type PricingApplyReq struct {
	PricingRuleReq
	// Note 本次调价的备注（写进留痕台账，便于事后解释「为什么改这一批」）。
	Note string `json:"note" form:"note"`
	// OperatorID 操作人 id。由 inbound 从会话覆盖写入（客户端传入的值被忽略），
	// 保证留痕的操作人不可伪造。
	OperatorID string `json:"-" form:"-"`
}

// ListPriceAdjustmentReq 调价留痕列表（按工程）。
type ListPriceAdjustmentReq struct {
	ProjectID string `form:"projectId"`
	Limit     int    `form:"limit"`
	// IncludeItems 批量读取各调价批次的明细，不逐批次查询。
	IncludeItems bool `form:"includeItems"`
}

// GetPriceAdjustmentReq 单批次留痕详情（含逐变体明细）。
//
// ProjectID 是工程隔离（DB-009）的作用域来源：留痕批次按工程隔离，读取必须落在
// 某个工程上。为空时由 service 走「唯一工程」兜底，多工程部署下会明确报参数错误
// （好过静默读到别的工程的留痕）。
type GetPriceAdjustmentReq struct {
	ID        string `form:"id" binding:"required"`
	ProjectID string `form:"projectId"`
}

// BindForm 统一预览与应用的表单参数；规则合法性仍由 service 校验。
func (r *PricingRuleReq) BindForm(vals url.Values) error {
	if vals.Has("ruleParams") {
		return bindRawJSONField(vals, "ruleParams", &r.RuleParams)
	}
	r.RuleParams = PricingParams(r.RuleType, r.Multiplier, r.Amount, r.Margin)
	return nil
}

// PricingParams 按规则类型从平铺的字符串参数拼规则参数（只取本类型用得到的键）。
//
// 非法数字**不给键** —— service 统一报「参数缺失或不是数字」，这里不重复一套校验；
// NaN / 正负无穷同档（它们进了算价会得到一整批 NaN 价格）。
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
