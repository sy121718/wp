package builder

import (
	"strings"
	"testing"
)

func fragmentCSSFor(t *testing.T, html string) string {
	t.Helper()
	css, _, err := uiAssetsForScan(collectHTMLScan(html), "/* ui css */", nil)
	if err != nil {
		t.Fatalf("组装控件资源失败: %v", err)
	}
	return css
}

func TestFragmentBaseStyleInjectedByHxTargetOnly(t *testing.T) {
	css := fragmentCSSFor(t, "<div data-site-cart>购物车</div>")
	if !strings.Contains(css, ".sky-cart-item {") {
		t.Fatalf("只写 hx-post 指向购物车片段的页面必须拿到购物车片段样式（页面上没有任何官方组件）；实际 CSS:\n%s", css)
	}
	if strings.Contains(css, ".sky-orders-item {") {
		t.Fatalf("页面没引用订单片段，不该带上订单片段样式（UIK-005 的按能力细分）；实际 CSS:\n%s", css)
	}

	if strings.Contains(css, "/* ui css */") {
		t.Fatalf("页面没有用任何控件类，控件基座样式不该被片段样式「顺带」注入；实际 CSS:\n%s", css)
	}
}

func TestFragmentBaseStyleNotInjectedWithoutFragmentReference(t *testing.T) {
	for _, html := range []string{
		"<div hx-get='/api/products?q=1'></div>",
		"<form hx-post='/cart'></form>",
		"<div data-fragments='/_fragments/ordersList'></div>",
		"<script>var s = '/_fragments/ordersList';</script>",
	} {
		css := fragmentCSSFor(t, html)
		if strings.Contains(css, ".sky-cart-item {") {
			t.Errorf("输入 %s 不该注入片段样式；实际 CSS:\n%s", html, css)
		}
	}
}

func TestFragmentBaseStyleInRealProductDocument(t *testing.T) {
	doc, err := RenderDocument(&CompiledPage{
		HTML:     "<div data-site-cart></div>",
		UIStyle:  "/* ui css */",
		Features: nil,
	})
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if !strings.Contains(doc, ".sky-cart-item {") {
		t.Fatalf("自定义入口的页面没拿到购物车片段样式；产物：\n%s", doc)
	}
}

func TestFragmentCapabilityTablesCoverEachOther(t *testing.T) {
	styled := FragmentStyleCapabilities()
	unstyled := FragmentUnstyledCapabilities()
	seen := map[string]bool{}
	for _, c := range styled {
		if seen[c] {
			t.Errorf("能力 %s 被两个族同时登记：命中时会重复注入两份样式", c)
		}
		seen[c] = true
		if _, dup := unstyled[c]; dup {
			t.Errorf("能力 %s 同时在「有样式族」与「无样式族」表里：两张表必须互斥", c)
		}
	}
	if len(styled) != 0 {
		t.Fatalf("有样式族只登记了 %d 个能力（购物车与订单均已迁 JSON）：表可能被清空", len(styled))
	}
	for name, reason := range unstyled {
		if len([]rune(strings.TrimSpace(reason))) < 12 {
			t.Errorf("无样式能力 %s 缺少现状说明（每个都要写清「为什么还没有样式 / 归属在哪」）", name)
		}
	}
}
