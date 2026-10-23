package productservice_test

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/utils"
)

// TestTagPageRowsCarryDrawerForm 标签页的行与抽屉初值由读模型装（控制器不再装配）。
//
// 判据是「换一个入口也拿得到同一份形状」：行里有规则取值（不是成品串），抽屉初值能回填
// 规则参数（days/minPrice/maxPrice 各自的文本），新建抽屉与行级抽屉同一份键集。
func TestTagPageRowsCarryDrawerForm(t *testing.T) {
	var recalc utils.JSONTime
	details := []*productdto.TagResp{{
		ID: "t1", Name: "近 30 天上新", Slug: "new-30", Kind: productenums.TagKindRule,
		RuleType:     productenums.TagRuleNewArrival,
		RuleParams:   []byte(`{"days":30}`),
		RuleValues:   productdto.RuleValuesResp{Days: 30, HasDays: true},
		ProductCount: 7, Sort: 2, RecalcAt: recalc,
	}}
	ruleTypes := []*productdto.TagRuleTypeResp{{Type: productenums.TagRuleNewArrival, ParamKeys: []string{"days"}}}

	rows := productservice.TagPageRows("p1", details, ruleTypes)
	if len(rows) != 1 {
		t.Fatalf("应有一行，实际 %d", len(rows))
	}
	row := rows[0]
	if !row.IsRule || row.RuleType != productenums.TagRuleNewArrival || !row.RuleValues.HasDays || row.RuleValues.Days != 30 {
		t.Fatalf("行应只带取值（规则类型 + RuleValues）：%+v", row)
	}
	if row.RuleDays != "30" {
		t.Fatalf("抽屉初值应回填规则参数文本，实际 %q", row.RuleDays)
	}
	if row.EditForm["Mode"] != "update" || row.EditForm["ID"] != "t1" || row.EditForm["RuleDays"] != "30" {
		t.Fatalf("行级抽屉初值不对：%+v", row.EditForm)
	}
	create := productservice.TagCreateForm("p1", ruleTypes)
	if create["Mode"] != "create" || create["ID"] != "" || create["Project"] != "p1" {
		t.Fatalf("新建抽屉初值不对：%+v", create)
	}
	if _, ok := create["RuleTypes"]; !ok {
		t.Fatal("新建抽屉必须带规则类型清单（前端据此组 ruleParams）")
	}
}
