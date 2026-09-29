// product_tag_rule.go — 自动标签的内置规则类型（issue #11）。
//
// 设计约束（本票验收 2）：自动标签**只接受内置规则类型与参数，不接受自由表达式**。
// 因此这里是一张写死的注册表，每个规则类型自带三件事：
//
//	Normalize  严格校验参数（未知键 / 类型不符 / 越界一律拒绝）并归一为落库形态；
//	Describe   把参数翻成人类可读描述（后台「商品标签」页展示，规则语义只有这一份）；
//	Evaluate   按规则求值，返回命中的商品 id（只读本模块表，工程过滤由 service 兜底）。
//
// 加规则 = 在这里加一条注册项（类型 + 参数白名单 + 求值 + 一句描述），
// 不需要新表、不需要改 DDL（091 的 CHECK 只兜底 kind/rule_type 的形状）。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/i18n"
	productmodel "go_wp/internal/module/product/model"
)

// 规则参数取值范围（越界即拒绝，不做静默裁剪）。
const (
	tagRuleDaysMin  = 1
	tagRuleDaysMax  = 365
	tagRulePriceMax = 1e9
)

// tagRule 一个内置规则类型。
type tagRule struct {
	// Type 落库的 rule_type。
	Type string
	// NameKey / Name 后台展示名：NameKey 是词条真源，Name 是中文兜底。展示名是**文案**
	//（内置规则的固定说法），与用户自定义的标签名（数据）无关。
	NameKey string
	Name    string
	// ParamsKey / Params 参数说明（后台展示，也是调用方唯一的参数文档来源）。
	ParamsKey string
	Params    string
	// Normalize 校验并归一参数（nil / 空对象按各规则自己的必需性判定）。
	Normalize func(params json.RawMessage) (json.RawMessage, error)
	// Describe 归一后的参数 → 当前语言的人类可读描述（tr 由调用点给）。
	Describe func(tr TranslateFunc, params json.RawMessage) string
	// Evaluate 求值：返回命中的商品 id（可能含其它工程，由 service 过滤到本工程）。
	Evaluate func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error)
}

// 内置标签规则的展示文案 key（词条见迁移 449_i18n_*）。
//
// 这些是**内置规则的固定说法**（后台展示名、参数说明、可读描述），不是数据 ——
// 与用户自定义的标签名无关。中文兜底留在规则表与下面的描述函数里。
const (
	tagKeyNewArrivalName        = "admin.product_tags.rule.newArrival.name"
	tagKeyNewArrivalParams      = "admin.product_tags.rule.newArrival.params"
	tagKeyNewArrivalDescribe    = "admin.product_tags.rule.newArrival.describe"
	tagKeyPriceRangeName        = "admin.product_tags.rule.priceRange.name"
	tagKeyPriceRangeParams      = "admin.product_tags.rule.priceRange.params"
	tagKeyPriceRangeDescribeBoth = "admin.product_tags.rule.priceRange.describeBoth"
	tagKeyPriceRangeDescribeMin = "admin.product_tags.rule.priceRange.describeMin"
	tagKeyPriceRangeDescribeMax = "admin.product_tags.rule.priceRange.describeMax"
	tagKeyOnSaleName            = "admin.product_tags.rule.onSale.name"
	tagKeyOnSaleParams          = "admin.product_tags.rule.onSale.params"
	tagKeyOnSaleDescribe        = "admin.product_tags.rule.onSale.describe"
	tagKeyParamsInvalid         = "admin.product_tags.rule.paramsInvalid"
	tagKeyUnknownRule           = "admin.product_tags.rule.unknown"
)

// tagRules 内置规则表（顺序即后台展示顺序）。新增规则只在这里追加。
var tagRules = []*tagRule{
	{
		Type:      productenums.TagRuleNewArrival,
		NameKey:   tagKeyNewArrivalName,
		Name:      "新品（上架 N 天内）",
		ParamsKey: tagKeyNewArrivalParams,
		Params:    "days：必填，1~365 的整数（上架时间以 products.published_at 为准）",
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			m, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			if err = rejectUnknownRuleKeys(m, "days"); err != nil {
				return nil, err
			}
			raw, ok := m["days"]
			if !ok {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRequired, "field", "days"))
			}
			days, derr := ruleInt(raw, "days")
			if derr != nil {
				return nil, derr
			}
			if days < tagRuleDaysMin || days > tagRuleDaysMax {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRange,
					"field", "days", "min", strconv.Itoa(tagRuleDaysMin),
					"max", strconv.Itoa(tagRuleDaysMax), "value", strconv.Itoa(days)))
			}
			return json.RawMessage(fmt.Sprintf("{%q:%d}", "days", days)), nil
		},
		Describe: func(tr TranslateFunc, params json.RawMessage) string {
			m, err := decodeRuleParams(params)
			if err != nil {
				return tr(tagKeyParamsInvalid, "参数不合法")
			}
			days, derr := ruleInt(m["days"], "days")
			if derr != nil {
				return tr(tagKeyParamsInvalid, "参数不合法")
			}
			return i18n.FillTranslate(tr, tagKeyNewArrivalDescribe, "上架 {days} 天内",
				map[string]string{"days": strconv.Itoa(days)})
		},
		Evaluate: func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error) {
			p, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			days, derr := ruleInt(p["days"], "days")
			if derr != nil {
				return nil, derr
			}
			since := time.Now().UTC().AddDate(0, 0, -days)
			return m.ListProductIDsPublishedSince(ctx, projectID, productenums.StatusPublished, since)
		},
	},
	{
		Type:   productenums.TagRulePriceRange,
		NameKey: tagKeyPriceRangeName,
		Name:    "价格区间（存在启用变体落在区间内）",
		ParamsKey: tagKeyPriceRangeParams,
		Params:    "minPrice / maxPrice：至少给一个，0~1000000000 的非负数，minPrice ≤ maxPrice",
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			m, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			if err = rejectUnknownRuleKeys(m, "minPrice", "maxPrice"); err != nil {
				return nil, err
			}
			if len(m) == 0 {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagPriceNeedsOne))
			}
			var minV, maxV *float64
			if raw, ok := m["minPrice"]; ok {
				v, verr := ruleFloat(raw, "minPrice")
				if verr != nil {
					return nil, verr
				}
				if v < 0 || v > tagRulePriceMax {
					return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRangePlain,
						"field", "minPrice", "min", "0", "max", formatRulePrice(tagRulePriceMax)))
				}
				minV = &v
			}
			if raw, ok := m["maxPrice"]; ok {
				v, verr := ruleFloat(raw, "maxPrice")
				if verr != nil {
					return nil, verr
				}
				if v < 0 || v > tagRulePriceMax {
					return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamRangePlain,
						"field", "maxPrice", "min", "0", "max", formatRulePrice(tagRulePriceMax)))
				}
				maxV = &v
			}
			if minV != nil && maxV != nil && *minV > *maxV {
				return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagPriceOrderWrong))
			}
			// 归一形态：只保留给了的键，键序固定（落库可读、可比对）。
			parts := make([]string, 0, 2)
			if minV != nil {
				parts = append(parts, fmt.Sprintf("%q:%s", "minPrice", formatRulePrice(*minV)))
			}
			if maxV != nil {
				parts = append(parts, fmt.Sprintf("%q:%s", "maxPrice", formatRulePrice(*maxV)))
			}
			return json.RawMessage("{" + strings.Join(parts, ",") + "}"), nil
		},
		Describe: func(tr TranslateFunc, params json.RawMessage) string {
			m, err := decodeRuleParams(params)
			if err != nil {
				return tr(tagKeyParamsInvalid, "参数不合法")
			}
			minV, _ := ruleFloat(m["minPrice"], "minPrice")
			maxV, _ := ruleFloat(m["maxPrice"], "maxPrice")
			_, hasMin := m["minPrice"]
			_, hasMax := m["maxPrice"]
			switch {
			case hasMin && hasMax:
				return i18n.FillTranslate(tr, tagKeyPriceRangeDescribeBoth, "价格 {min} ~ {max}", map[string]string{
					"min": formatRulePrice(minV), "max": formatRulePrice(maxV),
				})
			case hasMin:
				return i18n.FillTranslate(tr, tagKeyPriceRangeDescribeMin, "价格 ≥ {min}",
					map[string]string{"min": formatRulePrice(minV)})
			case hasMax:
				return i18n.FillTranslate(tr, tagKeyPriceRangeDescribeMax, "价格 ≤ {max}",
					map[string]string{"max": formatRulePrice(maxV)})
			}
			return tr(tagKeyParamsInvalid, "参数不合法")
		},
		Evaluate: func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error) {
			p, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			var minV, maxV *float64
			if raw, ok := p["minPrice"]; ok {
				v, verr := ruleFloat(raw, "minPrice")
				if verr != nil {
					return nil, verr
				}
				minV = &v
			}
			if raw, ok := p["maxPrice"]; ok {
				v, verr := ruleFloat(raw, "maxPrice")
				if verr != nil {
					return nil, verr
				}
				maxV = &v
			}
			return m.ListProductIDsByVariantPrice(ctx, minV, maxV)
		},
	},
	{
		Type:      productenums.TagRuleOnSale,
		NameKey:   tagKeyOnSaleName,
		Name:      "促销（有划线价）",
		ParamsKey: tagKeyOnSaleParams,
		Params:    "无参数（留空即 {}）；命中条件为存在启用变体且对比价高于售价",
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			m, err := decodeRuleParams(params)
			if err != nil {
				return nil, err
			}
			if err = rejectUnknownRuleKeys(m); err != nil {
				return nil, err
			}
			return json.RawMessage("{}"), nil
		},
		Describe: func(tr TranslateFunc, _ json.RawMessage) string {
			return tr(tagKeyOnSaleDescribe, "有划线价（对比价高于售价）")
		},
		Evaluate: func(ctx context.Context, m *productmodel.Model, projectID string, params json.RawMessage) ([]string, error) {
			return m.ListProductIDsWithDiscount(ctx)
		},
	},
}

// lookupTagRule 按类型取内置规则（未知类型返回 nil —— 调用方转成业务错误）。
func lookupTagRule(ruleType string) *tagRule {
	ruleType = strings.TrimSpace(ruleType)
	for _, r := range tagRules {
		if r.Type == ruleType {
			return r
		}
	}
	return nil
}

// tagRuleTypeOptions 内置规则类型清单（后台下拉与接口的唯一来源）。
func tagRuleTypeOptions(tr TranslateFunc) (out []*productdto.TagRuleTypeResp) {
	out = make([]*productdto.TagRuleTypeResp, 0, len(tagRules))
	for _, r := range tagRules {
		out = append(out, &productdto.TagRuleTypeResp{
			Type: r.Type, Name: tr(r.NameKey, r.Name), Params: tr(r.ParamsKey, r.Params),
		})
	}
	return out
}

// normalizeTagRuleParams 校验并归一规则参数（未知规则类型即拒绝）。
func normalizeTagRuleParams(ruleType string, params json.RawMessage) (out json.RawMessage, err error) {
	r := lookupTagRule(ruleType)
	if r == nil {
		return nil, ruleTypeErr(ruleType)
	}
	norm, nerr := r.Normalize(params)
	if nerr != nil {
		return nil, nerr
	}
	return norm, nil
}

// describeTagRule 规则的可读描述；手工标签返回空串。
//
// 未知类型（外部改库改坏 / 旧版本遗留）返回一句可读的「未知规则」而不是报错：
// 后台列表不该因为一条坏数据整页打不开。
func describeTagRule(tr TranslateFunc, ruleType string, params json.RawMessage) string {
	ruleType = strings.TrimSpace(ruleType)
	if ruleType == "" {
		return ""
	}
	r := lookupTagRule(ruleType)
	if r == nil {
		return fmt.Sprintf(tr(tagKeyUnknownRule, "未知规则类型：%s"), ruleType)
	}
	return r.Describe(tr, params)
}

// decodeRuleParams 规则参数必须是 JSON 对象（空 / null 按空对象处理）。
func decodeRuleParams(raw json.RawMessage) (m map[string]json.RawMessage, err error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]json.RawMessage{}, nil
	}
	if err = json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamsNotObject))
	}
	return m, nil
}

// rejectUnknownRuleKeys 拒绝白名单以外的键（自动标签只接受内置参数，不接受自由表达式）。
func rejectUnknownRuleKeys(m map[string]json.RawMessage, allowed ...string) (err error) {
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
	return ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagUnknownKeys,
		"keys", strings.Join(unknown, ", ")))
}

// ruleInt 取整型参数（JSON 数字且必须是整数；字符串形式的数字也拒绝 —— 类型不符即错）。
func ruleInt(raw json.RawMessage, key string) (v int, err error) {
	f, ferr := ruleFloat(raw, key)
	if ferr != nil {
		return 0, ferr
	}
	if f != float64(int(f)) {
		return 0, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamNotInteger, "field", key))
	}
	return int(f), nil
}

// ruleFloat 取数字参数（JSON 数字）。
func ruleFloat(raw json.RawMessage, key string) (v float64, err error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamNotNumber, "field", key))
	}
	f, ferr := strconv.ParseFloat(s, 64)
	if ferr != nil {
		return 0, ruleParamsErr(i18n.ErrorDetail(productenums.DetailTagParamNotNumber, "field", key))
	}
	return f, nil
}

// formatRulePrice 金额的规范文本（整数不带小数尾巴）。
func formatRulePrice(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// ruleParamsErr 参数错误的统一包装（消息带具体原因，便于后台直接显示）。
func ruleParamsErr(detail string) error {
	return fmt.Errorf("%s：%s", productenums.ErrTagRuleParamsInvalid, detail)
}

// ruleTypeErr 规则类型错误的统一包装。
func ruleTypeErr(ruleType string) error {
	if strings.TrimSpace(ruleType) == "" {
		return errors.New(productenums.ErrTagRuleTypeInvalid)
	}
	return fmt.Errorf("%s：%s", productenums.ErrTagRuleTypeInvalid,
		i18n.ErrorDetail(productenums.DetailTagUnknownRuleType, "type", ruleType))
}
