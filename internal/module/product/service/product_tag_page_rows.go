package productservice

// product_tag_page_rows.go — 标签页的行与抽屉初值（纯装配，形状只写一份）。
//
// 为什么在 service 而不是控制器：控制器是「随时可替换的入口」，只做绑定 → 调 → 出口；
// 把「行摊成模板键」写在入口里，换一个入口（MCP / 另一个视图层）就要重写一遍。
// 这里不取词：模板要的 t/tf 由渲染层注入（internal/text.Inject 会递归补给每一张 map）。

import (
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	json "go_wp/pkg/jsonx"
	"strings"
)

// TagPageRows 当页标签 → 模板行（行内带编辑抽屉初值）。
func TagPageRows(projectID string, details []*productdto.TagResp, ruleTypes []*productdto.TagRuleTypeResp) []productdto.TagRowResp {
	rows := make([]productdto.TagRowResp, 0, len(details))
	for _, tg := range details {
		if tg == nil {
			continue
		}
		row := tagPageRow(tg)
		row.EditForm = TagDrawerForm("update", projectID, &row, ruleTypes)
		rows = append(rows, row)
	}
	return rows
}

// TagCreateForm 新建抽屉的初值（与行级抽屉同源：同一份规则清单、同一份模板）。
func TagCreateForm(projectID string, ruleTypes []*productdto.TagRuleTypeResp) map[string]any {
	return TagDrawerForm("create", projectID, nil, ruleTypes)
}

// TagDrawerForm 标签抽屉的片段数据（create 时 row 为 nil）。
//
// 逐字段搬而不是按键名遍历：行类型改了名字这里会编译失败，键名写错则只有模板里静默少一段初值。
func TagDrawerForm(mode, projectID string, row *productdto.TagRowResp, ruleTypes []*productdto.TagRuleTypeResp) map[string]any {
	data := map[string]any{
		"Mode": mode, "Project": projectID,
		"RuleTypes": ruleTypes,
		"ID":        "", "Name": "", "Slug": "", "IsRule": false, "Sort": 0,
		"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
	}
	if row != nil {
		data["ID"], data["Name"], data["Slug"] = row.ID, row.Name, row.Slug
		data["IsRule"], data["Sort"], data["RuleType"] = row.IsRule, row.Sort, row.RuleType
		data["RuleDays"], data["RuleMinPrice"], data["RuleMaxPrice"] = row.RuleDays, row.RuleMinPrice, row.RuleMaxPrice
	}
	return data
}

// tagPageRow 标签 → 模板行（类型原值、规则参数、原样时间串；文案由模板按数据分支取词）。
func tagPageRow(t *productdto.TagResp) productdto.TagRowResp {
	return productdto.TagRowResp{
		ID: t.ID, Name: t.Name, Slug: t.Slug, Kind: t.Kind,
		IsRule:        t.Kind == productenums.TagKindRule,
		HasRuleParams: t.Kind == productenums.TagKindRule,
		KindRule:      productenums.TagKindRule,
		KindManual:    productenums.TagKindManual,
		RuleType:      t.RuleType,
		RuleValues:    t.RuleValues,
		RecalcAt:      t.RecalcAt.TimeOrZero(),
		ProductCount:  t.ProductCount, Sort: t.Sort,
		RuleDays:     ruleParamText(t.RuleParams, "days"),
		RuleMinPrice: ruleParamText(t.RuleParams, "minPrice"),
		RuleMaxPrice: ruleParamText(t.RuleParams, "maxPrice"),
	}
}

// ruleParamText 从规则参数里取某个键的文本（缺失返回空串，用于表单回填）。
func ruleParamText(params json.RawMessage, key string) string {
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
	return strings.Trim(strings.TrimSpace(string(raw)), "\"")
}
