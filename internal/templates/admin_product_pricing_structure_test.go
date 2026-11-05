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
	out, err := render(t, groupDSet(t), "admin/product/product_pricing", pricingPageData(), groupDRenderScope())
	if err != nil {
		t.Fatalf("定价页渲染失败: %v", err)
	}
	root, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("定价页输出无法解析为 HTML: %v", err)
	}
	return root, out
}

func TestPricingPreviewUsesHTMXForm(t *testing.T) {
	root, out := renderPricingPage(t)
	form := firstNode(root, func(n *html.Node) bool { return peEl(n, "form") && peHasAttr(n, "data-pricing-form") })
	if form == nil || peAttr(form, "hx-post") != "/api/product/pricing/preview" || !peHasAttr(form, "data-write") {
		t.Fatalf("预览表单必须是标准 HTMX 写表单：%s", snippet(out))
	}
	if !hasInputNamed(form, "csrf_token") {
		t.Fatal("写表单必需 CSRF 隐藏字段")
	}
	for _, old := range []string{"/admin/product-pricing/preview", "/admin/product-pricing/apply", "data-write-url"} {
		if strings.Contains(out, old) {
			t.Fatalf("保留了已退役的旧协议：%s", old)
		}
	}
	if !strings.Contains(out, "data-preview-lines") {
		t.Fatal("预览结果表体缺失")
	}
	if strings.Contains(out, "product-pricing.js") {
		t.Fatal("预览结果已由 Jet 片段渲染，不应再加载 JSON→DOM 脚本")
	}
}

func TestPricingApplyUsesHTMXButton(t *testing.T) {
	root, out := renderPricingPage(t)

	var apply *html.Node
	for _, n := range peAll(root, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "hx-post") == "/api/product/pricing/apply"
	}) {
		apply = n
	}
	if apply == nil {
		t.Fatalf("找不到挂 hx-post=/api/product/pricing/apply 的按钮；输出片段：%s", snippet(out))
	}
	if peAttr(apply, "hx-target") == "" || peAttr(apply, "hx-swap") == "" {
		t.Fatal("应用按钮必须声明成功目标与交换方式")
	}
	for _, key := range []string{"data-requires-js", "hidden", "disabled"} {
		if !peHasAttr(apply, key) {
			t.Fatalf("应用按钮缺 %s：无 JS 时必须不可点，而不是点了没反应", key)
		}
	}
	if peAttr(apply, "hx-confirm") == "" {
		t.Fatalf("应用调价会立刻写库，按钮必须带 hx-confirm")
	}

	preview := firstNode(root, func(n *html.Node) bool {
		return peEl(n, "button") && peAttr(n, "type") == "submit" && !peHasAttr(n, "hx-post")
	})
	if preview == nil {
		t.Fatalf("找不到试算按钮（应当提交宿主表单的 hx-post）")
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
