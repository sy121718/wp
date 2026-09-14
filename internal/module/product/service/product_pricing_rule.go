// product_pricing_rule.go — 定价工具的内置规则与尾数处理（issue #13）。
//
// 设计约束（验收 1）：只接受四个内置定价规则与四种尾数处理，**不接受自由表达式**。
// 因此这里是一张写死的注册表，每条规则自带四件事：
//
//	Normalize  严格校验参数（未知键 / 类型不符 / 越界一律拒绝）并归一为落库形态；
//	Describe   把参数翻成人类可读描述（后台展示，规则语义只有这一份）；
//	Compute    由成本价算出售价（纯函数，不碰数据库）；
//	FormValue  取归一后的核心参数值（后台表单回填用）。
//
// 算出的售价一律经 applyPricingRounding 处理尾数后才落库 —— 两条路径（预览与应用）
// 共用同一份实现，预览看到的数字就是应用后写进 product_variants.price 的数字。
package productservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// 定价参数与金额的取值范围（越界即拒绝，不做静默裁剪）。
const (
	// pricingMultiplierMin/Max 成本倍数：0.01~100（0 或负数会把售价算成 0 / 负数）。
	pricingMultiplierMin = 0.01
	pricingMultiplierMax = 100
	// pricingMarkupMax 成本加价金额上限。
	pricingMarkupMax = 1e9
	// pricingMarginMin/Max 目标毛利率：必须严格在 (0,1) 之间
	// （0 = 售价等于成本，1 = 售价无穷大，两端都要挡）。
	// 上限 0.95：margin 接近 1 时 cost/(1-margin) 浮点放大，审计 TX-013。
	pricingMarginMin = 0.0001
	pricingMarginMax = 0.95
	// pricingAmountMax 金额上限：product_variants.price 是 numeric(12,2)。
	pricingAmountMax = 9999999999.99
)

// pricingRule 一个内置定价规则类型。
type pricingRule struct {
	// Type 落库的 rule_type。
	Type string
	// Name 后台展示名。
	Name string
	// Params 参数说明（后台展示，也是调用方唯一的参数文档来源）。
	Params string
	// RequiresCost 规则是否依赖变体成本价（缺成本价的变体被跳过而不是整批失败）。
	RequiresCost bool
	// Normalize 校验并归一参数。
	Normalize func(params json.RawMessage) (json.RawMessage, error)
	// Describe 归一后的参数 → 人类可读描述。
	Describe func(params json.RawMessage) string
	// Compute 由成本价算售价（未归一 / 归一后的参数都能解析，调用方只用归一后的）。
	Compute func(cost float64, params json.RawMessage) (float64, error)
	// FormValue 核心参数值（后台表单回填的字符串形态）。
	FormValue func(params json.RawMessage) string
}

// pricingRules 内置定价规则表（顺序即后台展示顺序）。新增规则只在这里追加。
var pricingRules = []*pricingRule{
	{
		Type:         productenums.PricingRuleCostMultiple,
		Name:         "成本乘倍数",
		Params:       "multiplier：必填，0.01~100（售价 = 成本 × multiplier）",
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "multiplier", pricingMultiplierMin, pricingMultiplierMax)
		},
		Describe: func(params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "multiplier")
			if !ok {
				return "参数不合法"
			}
			return "成本 × " + formatPricingNumber(v)
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "multiplier")
			if !ok {
				return 0, pricingParamsErr("multiplier 缺失或不是数字")
			}
			return cost * v, nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "multiplier")
			return formatPricingNumber(v)
		},
	},
	{
		Type:         productenums.PricingRuleCostMarkup,
		Name:         "成本加价",
		Params:       "amount：必填，0~1000000000（售价 = 成本 + amount）",
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "amount", 0, pricingMarkupMax)
		},
		Describe: func(params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return "参数不合法"
			}
			return "成本 + " + formatPricingNumber(v)
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return 0, pricingParamsErr("amount 缺失或不是数字")
			}
			return cost + v, nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "amount")
			return formatPricingNumber(v)
		},
	},
	{
		Type:         productenums.PricingRuleTargetMargin,
		Name:         "目标毛利率",
		Params:       "margin：必填，0.0001~0.95 的小数（售价 = 成本 ÷ (1 - margin)，如 0.3 表示毛利率 30%）",
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "margin", pricingMarginMin, pricingMarginMax)
		},
		Describe: func(params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "margin")
			if !ok {
				return "参数不合法"
			}
			return "目标毛利率 " + formatPricingPercent(v)
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "margin")
			if !ok {
				return 0, pricingParamsErr("margin 缺失或不是数字")
			}
			if v <= 0 || v > pricingMarginMax {
				return 0, pricingParamsErr("margin 必须在 0.0001~0.95 之间")
			}
			return cost / (1 - v), nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "margin")
			return formatPricingNumber(v)
		},
	},
	{
		Type:         productenums.PricingRuleFixedPrice,
		Name:         "统一售价",
		Params:       "amount：必填，0~9999999999.99（不看成本，全部改为此售价）",
		RequiresCost: false,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "amount", 0, pricingAmountMax)
		},
		Describe: func(params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return "参数不合法"
			}
			return "统一售价 " + formatPricingNumber(v)
		},
		Compute: func(_ float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return 0, pricingParamsErr("amount 缺失或不是数字")
			}
			return v, nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "amount")
			return formatPricingNumber(v)
		},
	},
}

// lookupPricingRule 按类型取规则（未命中返回 nil）。
func lookupPricingRule(ruleType string) *pricingRule {
	ruleType = strings.TrimSpace(ruleType)
	for _, r := range pricingRules {
		if r.Type == ruleType {
			return r
		}
	}
	return nil
}

// normalizePricingRuleParams 校验并归一某规则的参数（未知类型按规则类型错误返回）。
func normalizePricingRuleParams(ruleType string, params json.RawMessage) (norm json.RawMessage, err error) {
	r := lookupPricingRule(ruleType)
	if r == nil {
		return nil, pricingRuleTypeErr(ruleType)
	}
	return r.Normalize(params)
}

// describePricingRule 规则的可读描述；未知类型给一句可读说明而不是报错
// （后台列表不该因为一条坏数据整页打不开）。
func describePricingRule(ruleType string, params json.RawMessage) string {
	ruleType = strings.TrimSpace(ruleType)
	if ruleType == "" {
		return ""
	}
	r := lookupPricingRule(ruleType)
	if r == nil {
		return "未知定价规则：" + ruleType
	}
	return r.Describe(params)
}

// normalizeSinglePricingParam 单参数规则的统一校验：白名单只允许这一个键、必填、数字、范围内。
func normalizeSinglePricingParam(params json.RawMessage, key string, min, max float64) (out json.RawMessage, err error) {
	m, derr := decodeRuleParams(params)
	if derr != nil {
		return nil, pricingParamsErr("参数必须是 JSON 对象")
	}
	if err = rejectUnknownPricingKeys(m, key); err != nil {
		return nil, err
	}
	raw, ok := m[key]
	if !ok {
		return nil, pricingParamsErr(key + " 必填")
	}
	v, ferr := ruleFloat(raw, key)
	if ferr != nil {
		return nil, pricingParamsErr(key + " 必须是数字")
	}
	if v < min || v > max {
		return nil, pricingParamsErr(fmt.Sprintf("%s 必须在 %s~%s 之间，实际 %s",
			key, formatPricingNumber(min), formatPricingNumber(max), formatPricingNumber(v)))
	}
	return json.RawMessage(fmt.Sprintf("{%q:%s}", key, formatPricingNumber(v))), nil
}

// rejectUnknownPricingKeys 拒绝白名单以外的键（定价工具只接受内置参数，不接受自由表达式）。
func rejectUnknownPricingKeys(m map[string]json.RawMessage, allowed ...string) (err error) {
	allow := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allow[k] = true
	}
	unknown := make([]string, 0, len(m))
	for k := range m {
		if !allow[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return pricingParamsErr("不支持的参数键：" + strings.Join(unknown, ", "))
}

// pricingParamFloat 取归一后参数里的数字（缺失 / 非法返回 false）。
func pricingParamFloat(params json.RawMessage, key string) (v float64, ok bool) {
	m, err := decodeRuleParams(params)
	if err != nil {
		return 0, false
	}
	raw, exists := m[key]
	if !exists {
		return 0, false
	}
	v, ferr := ruleFloat(raw, key)
	if ferr != nil {
		return 0, false
	}
	return v, true
}

// —— 尾数处理 ——

// pricingRounding 一种尾数处理方式。
type pricingRounding struct {
	// Value 落库的 rounding。
	Value string
	// Name 后台展示名。
	Name string
	// Apply 金额 → 分（整数）。除 none（四舍五入到分）外一律**向上取**：
	// 尾数处理只抬不降，保证按规则算出的售价不会因为凑尾数被压低。
	Apply func(amount float64) int64
}

// pricingRoundings 尾数处理表（顺序即后台展示顺序）。
var pricingRoundings = []*pricingRounding{
	{
		Value: productenums.PricingRoundingNone,
		Name:  "不舍入（四舍五入到分）",
		Apply: func(amount float64) int64 { return int64(math.Round(amount * 100)) },
	},
	{
		Value: productenums.PricingRoundingInteger,
		Name:  "向上取整到元",
		// 12.01 → 13.00；12.00 → 12.00。
		Apply: func(amount float64) int64 { return ceilToStep(pricingCeilCents(amount), 100) },
	},
	{
		Value: productenums.PricingRoundingEnd9,
		Name:  "尾数 9（向上取到角位为 9）",
		// 12.34 → 12.90；12.90 → 12.90；12.91 → 13.90。
		Apply: func(amount float64) int64 { return ceilToCentsRemainder(pricingCeilCents(amount), 90) },
	},
	{
		Value: productenums.PricingRoundingEnd99,
		Name:  "尾数 99（向上取到分为 99）",
		// 12.34 → 12.99；12.99 → 12.99；13.00 → 13.99。
		Apply: func(amount float64) int64 { return ceilToCentsRemainder(pricingCeilCents(amount), 99) },
	},
}

// lookupPricingRounding 按取值取尾数处理（空串按 none，未命中返回 nil）。
func lookupPricingRounding(rounding string) *pricingRounding {
	rounding = strings.TrimSpace(rounding)
	if rounding == "" {
		rounding = productenums.PricingRoundingNone
	}
	for _, r := range pricingRoundings {
		if r.Value == rounding {
			return r
		}
	}
	return nil
}

// normalizePricingRounding 校验尾数处理取值（空串归一为 none）。
func normalizePricingRounding(rounding string) (out string, err error) {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return "", pricingRoundingErr(rounding)
	}
	return r.Value, nil
}

// describePricingRounding 尾数处理的可读名（未知取值给一句可读说明）。
func describePricingRounding(rounding string) string {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return "未知尾数处理：" + strings.TrimSpace(rounding)
	}
	return r.Name
}

// applyPricingRounding 金额 → 尾数处理后的金额。
//
// 先换算成分（整数）再过尾数规则：全程整数运算，避免 12.90 这类十进制小数
// 在二进制浮点里表示不出来导致 12.34 向上取到 12.89 的偏差。
func applyPricingRounding(amount float64, rounding string) (out float64, err error) {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return 0, pricingRoundingErr(rounding)
	}
	return float64(r.Apply(amount)) / 100, nil
}

// pricingCeilCents 金额向上取到分（先减一点浮点尾差，避免 12.9 存成 12.900000000000002 时多进一分）。
func pricingCeilCents(amount float64) int64 {
	return int64(math.Ceil(amount*100 - 1e-9))
}

// ceilToStep 分值向上取到 step 的整数倍。
func ceilToStep(cents, step int64) int64 {
	if step <= 1 {
		return cents
	}
	rem := cents % step
	if rem < 0 {
		rem += step
	}
	if rem == 0 {
		return cents
	}
	return cents + (step - rem)
}

// ceilToCentsRemainder 分值向上取到「模 100 等于 remainder」的第一个分值。
//
// 尾数 9 用 remainder=90（角位为 9），尾数 99 用 remainder=99。
func ceilToCentsRemainder(cents, remainder int64) int64 {
	rem := cents % 100
	if rem < 0 {
		rem += 100
	}
	if rem <= remainder {
		return cents + (remainder - rem)
	}
	return cents + (100 - rem) + remainder
}

// —— 作用范围 ——

// normalizePricingScope 校验作用范围取值。
func normalizePricingScope(scope string) (out string, err error) {
	switch strings.TrimSpace(scope) {
	case productenums.PricingScopeSKU:
		return productenums.PricingScopeSKU, nil
	case productenums.PricingScopeProduct:
		return productenums.PricingScopeProduct, nil
	case productenums.PricingScopeFilter:
		return productenums.PricingScopeFilter, nil
	}
	return "", errors.New(productenums.ErrPricingScopeInvalid)
}

// describePricingScope 作用范围的可读名。
func describePricingScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case productenums.PricingScopeSKU:
		return "单个 SKU"
	case productenums.PricingScopeProduct:
		return "单个商品的全部变体"
	case productenums.PricingScopeFilter:
		return "筛选出的商品集"
	}
	return "未知作用范围"
}

// —— 后台选项（下拉与说明的唯一来源）——

// pricingRuleTypeOptions 内置定价规则清单。
func pricingRuleTypeOptions() (out []*productdto.PricingRuleTypeResp) {
	out = make([]*productdto.PricingRuleTypeResp, 0, len(pricingRules))
	for _, r := range pricingRules {
		out = append(out, &productdto.PricingRuleTypeResp{
			Type: r.Type, Name: r.Name, Params: r.Params, RequiresCost: r.RequiresCost,
		})
	}
	return out
}

// pricingRoundingOptions 尾数处理清单。
func pricingRoundingOptions() (out []*productdto.PricingRoundingOptionResp) {
	out = make([]*productdto.PricingRoundingOptionResp, 0, len(pricingRoundings))
	for _, r := range pricingRoundings {
		out = append(out, &productdto.PricingRoundingOptionResp{Value: r.Value, Name: r.Name})
	}
	return out
}

// —— 文本工具 ——

// formatPricingNumber 数值的规范文本（整数不带小数尾巴）。
func formatPricingNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// formatPricingPercent 毛利率的小数 → 百分比文本（0.3 → 30%）。
func formatPricingPercent(v float64) string { return formatPricingNumber(v*100) + "%" }

// pricingParamsErr 参数错误的统一包装（消息带具体原因，便于后台直接显示）。
func pricingParamsErr(detail string) error {
	return fmt.Errorf("%s：%s", productenums.ErrPricingRuleParamsInvalid, detail)
}

// pricingRuleTypeErr 规则类型错误的统一包装。
func pricingRuleTypeErr(ruleType string) error {
	if strings.TrimSpace(ruleType) == "" {
		return errors.New(productenums.ErrPricingRuleTypeInvalid)
	}
	return fmt.Errorf("%s：%s", productenums.ErrPricingRuleTypeInvalid, ruleType)
}

// pricingRoundingErr 尾数处理错误的统一包装。
func pricingRoundingErr(rounding string) error {
	if strings.TrimSpace(rounding) == "" {
		return errors.New(productenums.ErrPricingRoundingInvalid)
	}
	return fmt.Errorf("%s：%s", productenums.ErrPricingRoundingInvalid, rounding)
}
