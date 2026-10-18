// product_tag_row.go — 标签行与规则参数的**视图装配**（模板行 = 业务数据 + 徽标分档 + 文案事实）。
//
// 为什么在 service：规则描述、命中数与「重算时间怎么显示」都是业务结论的表示；
// 控制器各拼一遍就会在两处（列表页与命中片段）出现不同的行形状 —— 而差异不报错，
// 只让运营看到两套说法。文案槽给**事实**（key + 中文兜底），取词仍在模板的 tr 里做。
package productservice

import (
	"strings"
	"time"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	json "go_wp/pkg/jsonx"
)

// TagPageRow 标签 → 模板行（规则描述与命中数都由服务端算好，模板不做第二套解释；
// 命中商品本身不在这里，展开时才由命中片段给 —— 审计 PERF-02）。
//
// RecalcAt 是**数据**（已格式化的时间串，未重算时为空），「未重算过」那句话才是文案。
func TagPageRow(t *productdto.TagResp) productdto.TagRowResp {
	// 类型 → 文案事实的映射在 productenums.TagKindLabel。
	kindKey, kindFallback := productenums.TagKindLabel(t.Kind)
	recalcAt, recalcKey, recalcFallback := RecalcFact(t.RecalcAt)
	return productdto.TagRowResp{
		ID: t.ID, Name: t.Name, Slug: t.Slug, Kind: t.Kind,
		KindLabelKey: kindKey, KindLabelFallback: kindFallback,
		IsRule:        t.Kind == productenums.TagKindRule,
		HasRuleParams: t.Kind == productenums.TagKindRule,
		KindTone:      tagKindTone(t.Kind),
		KindRule:      productenums.TagKindRule,
		KindManual:    productenums.TagKindManual,
		RuleType:      t.RuleType,
		RuleLabel:     t.RuleLabel,
		RecalcAt:      recalcAt, RecalcLabelKey: recalcKey, RecalcLabelFallback: recalcFallback,
		ProductCount: t.ProductCount, Sort: t.Sort,
		RuleDays:     RuleParamText(t.RuleParams, "days"),
		RuleMinPrice: RuleParamText(t.RuleParams, "minPrice"),
		RuleMaxPrice: RuleParamText(t.RuleParams, "maxPrice"),
	}
}

// tagKindTone 标签类型 → 徽标分档（规则型标签 → info，普通标签中性）。
func tagKindTone(kind string) string {
	if kind == productenums.TagKindRule {
		return "info"
	}
	return "mute"
}

// RecalcFact 重算时间 → （数据串, 文案事实）。
//
// 输入是 RFC3339，这里压成「2006-01-02 15:04」：既好读，又不会因为那一长串时间戳
// 在窄屏折叠行里连成不可断行的 token 把卡片撑破。时间是数据（格式固定，走第一个返回值），
// 只有「没重算过」那句话是文案 —— 给 key + 中文兜底，取词在模板的 tr 里做。
func RecalcFact(at string) (label, key, fallback string) {
	key, fallback = productenums.TagRecalcLabel(at)
	return RecalcAtText(at), key, fallback
}

// RecalcAtText 重算时间的数据串：RFC3339 → 「2006-01-02 15:04」。
//
// 空值给空串（模板据它渲染「—」那一档），不可解析时**原样返回** —— 至少把原值显示出来，
// 留白会让人以为「没有这个数据」。
func RecalcAtText(at string) string {
	at = strings.TrimSpace(at)
	if at == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t.Local().Format("2006-01-02 15:04")
	}
	return at
}

// RuleParamText 从规则参数里取某个键的文本（缺失返回空串，用于表单回填）。
func RuleParamText(params json.RawMessage, key string) string {
	if len(params) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(params, &m); err != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	return strings.Trim(s, "\"")
}
