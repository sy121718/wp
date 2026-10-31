package templates

import (
	"strings"
	"testing"
)

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

			page: "product_pricing",
			data: groupDData(map[string]any{
				"Scope": map[string]any{"ProjectID": "pr1", "Projects": groupDProjects()},
				"Rules": pricingFieldsRules(), "Roundings": pricingFieldsRoundings(),
				"History": []map[string]any{},
			}),
		},
		{

			page: "products",
			data: productCatalogData(map[string]any{"req_path": "/admin/products", "Rules": pricingFieldsRules(), "Roundings": pricingFieldsRoundings()}),
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
