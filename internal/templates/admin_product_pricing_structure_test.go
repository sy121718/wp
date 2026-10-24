package templates

// admin_product_pricing_structure_test.go — 定价工具页的写动作用 JSON 写基座（批次 D）。
//
// 这一页与其它页不同：**同一张表单承载两个动作**。
//   · 试算（preview）是只读的，结果是一张表格，必须整页重渲才渲染得出来 —— 保留原生提交；
//   · 应用调价（apply）是写动作，走 /api/product/pricing/apply，由 ui/write-base.js 提交。
//
// 因此判据的重点不是「表单挂没挂基座」，而是**表单本身绝不能挂**：一旦 form 带
// data-write-api，试算也会被拦进 fetch，用户再也看不到试算表（点了没反应/没表格）。
import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// pricingPageData 定价页的渲染数据（与 ProductPricingPageResp 同形）。
//
// HasPreview=false：本测试只钉写通道的接线，试算表那一段与它无关，
// 少一份假明细就少一处「模板字段改了却没人发现」的假绿来源。
func pricingPageData() map[string]any {
	return groupDData(map[string]any{
		"SelectedProject": "pr1",
		"Projects":        groupDProjects(),
		"Rules": []map[string]any{
			{"Type": "multiplier", "Name": "成本乘倍数", "Params": "multiplier", "RequiresCost": true},
		},
		"Roundings": []map[string]any{
			{"Value": "0.01", "Name": "保留两位小数"},
		},
		"Form": map[string]any{
			"ProjectID": "pr1", "RuleType": "multiplier", "Multiplier": "1.2",
			"Amount": "", "Margin": "", "Rounding": "0.01", "Scope": "filter",
			"TargetID": "", "Status": "", "Keyword": "",
			"CategoryID": "", "BrandID": "", "TagID": "", "Note": "",
		},
		"HasPreview":     false,
		"PreviewSummary": map[string]any{},
		"PreviewRows":    []map[string]any{},
		"History":        []map[string]any{},
		"Err":            "",
		"ListQuery":      "project=pr1",
	})
}

// renderPricingPage 渲染并把输出解析成 DOM。
func renderPricingPage(t *testing.T) (*html.Node, string) {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/product/product_pricing", pricingPageData())
	if err != nil {
		t.Fatalf("定价页渲染失败: %v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("定价页输出无法解析为 HTML: %v", err)
	}
	return root, out
}

// TestPricingFormKeepsNativePreview 表单本身保持原生提交（试算要整页重渲才出表格）。
func TestPricingFormKeepsNativePreview(t *testing.T) {
	root, out := renderPricingPage(t)

	form := firstNode(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "action") == "/admin/product-pricing/preview"
	})
	if form == nil {
		t.Fatalf("找不到 action=/admin/product-pricing/preview 的表单；输出片段：%s", snippet(out))
	}
	if peHasAttr(form, "data-write-url") {
		t.Fatalf("表单本身不能挂 data-write-api —— 那会把「预览试算」也拦进 fetch，试算表就没了")
	}
	// CSRF 隐藏域必须留着：试算仍是原生 POST，没有基座的 X-CSRF-Token 头可用。
	if !hasInputNamed(form, "csrf_token") {
		t.Fatalf("试算仍是原生表单提交，csrf_token 隐藏域不能删")
	}
	// 旧的应用端点不该再出现在任何地方（formaction 也不行）。
	if strings.Contains(out, "/admin/product-pricing/apply") {
		t.Fatalf("输出里仍有已删除的页面端点 /admin/product-pricing/apply")
	}
}

// TestPricingApplyUsesJSONBase 应用调价按钮单独挂基座（submitter 级覆盖）。
func TestPricingApplyUsesJSONBase(t *testing.T) {
	root, out := renderPricingPage(t)

	var apply *html.Node
	for _, n := range peAll(root, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "data-write-url") == "/api/product/pricing/apply"
	}) {
		apply = n
	}
	if apply == nil {
		t.Fatalf("找不到挂 hx-post=/api/product/pricing/apply 的按钮；输出片段：%s", snippet(out))
	}
	if got := peAttr(apply, "data-write-url"); got != "/api/product/pricing/apply" {
		t.Fatalf("应用按钮的 data-write-url = %q，want /api/product/pricing/apply", got)
	}
	if !peHasAttr(apply, "data-write-reload") {
		t.Fatal("应用按钮缺 data-write-reload —— 成功后结论不会跨刷新显示")
	}
	for _, key := range []string{"data-requires-js", "hidden", "disabled"} {
		if !peHasAttr(apply, key) {
			t.Fatalf("应用按钮缺 %s：无 JS 时必须不可点，而不是点了没反应", key)
		}
	}
	if peAttr(apply, "data-write-confirm") == "" {
		t.Fatalf("应用调价会立刻写库，按钮必须带 data-confirm")
	}

	// 试算按钮不能带端点：它与应用按钮同在一个表单里，被基座接走就再也出不了试算表。
	preview := firstNode(root, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "type") == "submit" && !peHasAttr(n, "data-write-url")
	})
	if preview == nil {
		t.Fatalf("找不到试算按钮（应当是不带 data-write-api 的 submit 按钮）")
	}
}

// TestPricingPageStatusAndFlashTargets 结论行与 flash 落点各一处，且结论行在表单内。
func TestPricingPageStatusAndFlashTargets(t *testing.T) {
	root, out := renderPricingPage(t)

	form := firstNode(root, func(n *html.Node) bool {
		return peEl(n, "form") && peAttr(n, "action") == "/admin/product-pricing/preview"
	})
	if form == nil {
		t.Fatalf("找不到改价表单；输出片段：%s", snippet(out))
	}
	// 基座的 statusOf 只在 form 内（或 [data-write-host] 内）找结论行 —— 放外面等于没有。
	if got := len(peAll(form, func(n *html.Node) bool { return peHasAttr(n, "data-write-status") })); got != 1 {
		t.Fatalf("改价表单内应恰有 1 个 [data-write-status] 结论行，实际 %d", got)
	}
	// 结论跨刷新已改由 sessionStorage 承载（新基座），页面不再需要落点元素。
}

// firstNode 返回第一个满足条件的节点（找不到返回 nil）。
func firstNode(root *html.Node, pred func(*html.Node) bool) *html.Node {
	if got := peAll(root, pred); len(got) > 0 {
		return got[0]
	}
	return nil
}

// hasInputNamed 判断表单里有没有指定 name 的 input（值为空串也算存在）。
func hasInputNamed(form *html.Node, name string) bool {
	return firstNode(form, func(n *html.Node) bool {
		return peEl(n, "input") && peAttr(n, "name") == name
	}) != nil
}

// snippet 给失败信息一段可读的上下文（模板改坏时比「找不到节点」有用得多）。
func snippet(out string) string {
	s := strings.TrimSpace(out)
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}
