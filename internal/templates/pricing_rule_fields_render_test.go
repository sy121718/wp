package templates

import (
	"strings"
	"testing"
)

// pricing_rule_fields_render_test.go — 「按规则改价」两行片段的行标签契约（**两个调用点都渲染**）。
//
// 背景：片段此前把 .hint 当行标签（`<span class="hint">规则</span>`），而 .hint 是「说明文字」类 ——
// 它做标签时读屏只能靠包裹关系猜、脚本也定位不到，同一张表单里因此并存两种行标签样式
//（本页其它行早已是 label.form-label + for/id）。改成 label + for/id 之后要钉住两件事：
//  1. 片段里 label 与控件的 for/id **成对**（少一半等于标签点不动、读屏也串不上）；
//  2. id 在同一页只出现一次（片段被两个调用点 include，页面上重复 id 会让 for 指向第一个）。
//
// 为什么必须渲染而不能只读源码：片段经 `{{include}}` 进入两个页面
//（product_pricing.html 独立定价页、products.html 列表页的批量改价抽屉），
// 只有渲染才能确认 include 真的生效、且 Jet 没有在中间某行中断
//（那种情况 HTTP 仍 200、后半截整块消失）。渲染外壳与断言工具复用同包的 groupDData /
// renderAdminPage。

// pricingFieldsForm 片段需要的表单初值（键名与 PricingRuleReq 的字段一致）。
func pricingFieldsForm() map[string]any {
	return map[string]any{
		"RuleType": "markup", "Multiplier": "2", "Amount": "1", "Margin": "0.3",
		"Rounding": "none", "Scope": "sku", "TargetID": "", "Status": "",
		"Keyword": "", "CategoryID": "", "BrandID": "", "TagID": "", "Note": "",
	}
}

func pricingFieldsRules() []map[string]any {
	return []map[string]any{{"Type": "markup", "Name": "成本加价", "RequiresCost": true, "Params": "amount"}}
}

func pricingFieldsRoundings() []map[string]any {
	return []map[string]any{{"Value": "none", "Name": "不处理"}}
}

func TestPricingRuleFieldsLabelContract(t *testing.T) {
	pages := []struct {
		page string
		data map[string]any
	}{
		{
			// 调用点一：独立定价工具页。
			page: "product_pricing",
			data: groupDData(map[string]any{
				"SelectedProject": "pr1", "Err": "", "Applied": "",
				"Projects": groupDProjects(),
				"Form":     pricingFieldsForm(), "Rules": pricingFieldsRules(), "Roundings": pricingFieldsRoundings(),
				"HasPreview": false, "History": []map[string]any{},
			}),
		},
		{
			// 调用点二：商品列表页的批量改价抽屉（片段在 `<template id="tpl-product-pricing">` 内，
			// 由表单里的 Rules / Roundings / Form 三个键决定要不要渲染）。
			page: "products",
			data: groupDData(map[string]any{
				"SelectedProject": "pr1", "Err": "",
				"Projects":         groupDProjects(),
				"WarehouseOptions": []map[string]any{{"ID": "w1", "Label": "苏州仓"}},
				"Products":         []map[string]any{groupDProductRow()},
				"Form":             pricingFieldsForm(), "Rules": pricingFieldsRules(), "Roundings": pricingFieldsRoundings(),
			}),
		},
	}

	for _, tc := range pages {
		out := renderAdminPage(t, tc.page, tc.data)

		for _, want := range []string{
			`for="pricing-rule-type"`, `id="pricing-rule-type"`,
			`for="pricing-rounding"`, `id="pricing-rounding"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s：片段应渲染 %s（label 与控件成对），实际未见", tc.page, want)
			}
		}
		for _, id := range []string{`id="pricing-rule-type"`, `id="pricing-rounding"`} {
			if got := strings.Count(out, id); got != 1 {
				t.Errorf("%s：%s 应在一个页面上只出现一次，实际 %d 次", tc.page, id, got)
			}
		}
		// 行标签不再用 .hint（那是说明文字类）：两行都必须走 label.form-label。
		for _, forbidden := range []string{
			`<span class="hint">规则</span>`,
			`<span class="hint">尾数</span>`,
		} {
			if strings.Contains(out, forbidden) {
				t.Errorf("%s：行标签仍写成 %s（应改成 label.form-label + for/id）", tc.page, forbidden)
			}
		}
	}
}
