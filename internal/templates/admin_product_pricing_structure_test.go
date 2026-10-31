package templates

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func pricingPageData() map[string]any {
	return groupDData(map[string]any{
		"Scope":     map[string]any{"ProjectID": "pr1", "Projects": groupDProjects()},
		"Rules":     []map[string]any{{"Type": "cost_multiple", "RequiresCost": true}},
		"Roundings": []map[string]any{{"Value": "none"}},
		"History":   []map[string]any{},
	})
}

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

func TestPricingPreviewUsesJSONBase(t *testing.T) {
	root, out := renderPricingPage(t)
	form := firstNode(root, func(n *html.Node) bool { return peEl(n, "form") && peHasAttr(n, "data-pricing-form") })
	if form == nil || peAttr(form, "data-write-url") != "/api/product/pricing/preview" || peAttr(form, "data-write") != "custom" {
		t.Fatalf("预览必须复用JSON写基座：%s", snippet(out))
	}
	if !hasInputNamed(form, "csrf_token") {
		t.Fatal("JSON FormData请求仍需CSRF隐藏字段")
	}
	for _, old := range []string{"/admin/product-pricing/preview", "/admin/product-pricing/apply"} {
		if strings.Contains(out, old) {
			t.Fatalf("保留了页面POST路径：%s", old)
		}
	}
	if !strings.Contains(out, "data-preview-lines") || !strings.Contains(out, "/static/js/ui/product-pricing.js") {
		t.Fatal("缺少消费JSON结果的节点与控件")
	}
}

func TestPricingApplyUsesJSONBase(t *testing.T) {
	root, out := renderPricingPage(t)

	var apply *html.Node
	for _, n := range peAll(root, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "data-write-url") == "/api/product/pricing/apply"
	}) {
		apply = n
	}
	if apply == nil {
		t.Fatalf("找不到挂 data-write-url=/api/product/pricing/apply 的按钮；输出片段：%s", snippet(out))
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
		t.Fatalf("应用调价会立刻写库，按钮必须带 data-write-confirm")
	}

	preview := firstNode(root, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "type") == "submit" && !peHasAttr(n, "data-write-url")
	})
	if preview == nil {
		t.Fatalf("找不到试算按钮（应当继承 form 的 data-write-url）")
	}
}

func TestPricingPageStatusAndFlashTargets(t *testing.T) {
	root, out := renderPricingPage(t)

	form := firstNode(root, func(n *html.Node) bool {
		return peEl(n, "form") && peHasAttr(n, "data-pricing-form")
	})
	if form == nil {
		t.Fatalf("找不到改价表单；输出片段：%s", snippet(out))
	}

	if got := len(peAll(form, func(n *html.Node) bool { return peHasAttr(n, "data-write-status") })); got != 1 {
		t.Fatalf("改价表单内应恰有 1 个 [data-write-status] 结论行，实际 %d", got)
	}

}

func firstNode(root *html.Node, pred func(*html.Node) bool) *html.Node {
	if got := peAll(root, pred); len(got) > 0 {
		return got[0]
	}
	return nil
}

func hasInputNamed(form *html.Node, name string) bool {
	return firstNode(form, func(n *html.Node) bool {
		return peEl(n, "input") && peAttr(n, "name") == name
	}) != nil
}

func snippet(out string) string {
	s := strings.TrimSpace(out)
	if len(s) > 600 {
		return s[:600] + "…"
	}
	return s
}
