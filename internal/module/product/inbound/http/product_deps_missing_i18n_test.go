package producthttp

import (
	"os"
	"strings"
	"testing"
)

// product_deps_missing_i18n_test.go — 详情页模板页「依赖未装配」这条提示的归属。
//
// 判据：**文案属前端层**。Go 侧只交「能力未装配」这个事实（Ready=false），
// 提示串由模板自己的取词给出；控制器不再拼成品串、也不再为它造第二套失败槽。
//
// 本文件守两件错了会静默的事：
//
//	· 未装配时视图必须是 Ready=false（否则模板走正常分支，运营看不到任何原因）；
//	· 模板里必须有这条提示的取词（删了块会让未装配页变成一具空壳，200 且无话可说）。
func TestDepsMissingPromptLivesInTemplate(t *testing.T) {
	view := detailTemplateUnassembledView()
	if view.Ready {
		t.Fatal("依赖未装配的视图必须是 Ready=false：模板据此就地说明原因")
	}
	body, err := os.ReadFile("../../../../templates/admin/product/product_detail_template.html")
	if err != nil {
		t.Fatalf("读模板失败: %v", err)
	}
	if !strings.Contains(string(body), "admin.product_detail_template.depsMissing") {
		t.Fatal("模板缺少未装配提示的取词：Go 侧已不再提供这句文案")
	}
}
