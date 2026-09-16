package builder

// fragment_base_test.go — 片段基座样式的注入判据与粒度（审计 UIK-003 第二层 / UIK-005）。
//
// 三条验收直接来自条目原文：
//  ① 「不使用官方组件而直接写 hx-get 的页面，片段样式正常」—— 判据必须是页面特征，
//     不是「页面上放了哪个组件」；
//  ② 「只用购物车的页面不带订单片段样式」—— 按片段能力分族；
//  ③ 片段样式在产物里只出现一次（迁移前靠 CSSBuckets 的规则去重，现在靠一次拼接）。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// fragmentCSSFor 走一遍真实的注入判定（无脚本源模式：本文件只关心 CSS）。
func fragmentCSSFor(t *testing.T, html string) string {
	t.Helper()
	css, _, err := uiAssetsForScan(collectHTMLScan(html), "/* ui css */", nil)
	if err != nil {
		t.Fatalf("组装控件资源失败: %v", err)
	}
	return css
}

// TestFragmentBaseStyleInjectedByHxTargetOnly 判据是「页面指向 /_fragments/ 的 htmx 请求」，
// 而不是「用了哪个组件」——UIK-005 的原始症状就是自定义入口拿不到样式。
func TestFragmentBaseStyleInjectedByHxTargetOnly(t *testing.T) {
	css := fragmentCSSFor(t, "<button type='button' hx-post='/_fragments/cartAdd?projectId=p1'>加购</button>")
	if !strings.Contains(css, ".sky-cart-item {") {
		t.Fatalf("只写 hx-post 指向购物车片段的页面必须拿到购物车片段样式（页面上没有任何官方组件）；实际 CSS:\n%s", css)
	}
	if strings.Contains(css, ".sky-orders-item {") {
		t.Fatalf("页面没引用订单片段，不该带上订单片段样式（UIK-005 的按能力细分）；实际 CSS:\n%s", css)
	}
	// 片段样式与控件基座彼此独立：没有控件类时 ui.css 不该被带进来。
	if strings.Contains(css, "/* ui css */") {
		t.Fatalf("页面没有用任何控件类，控件基座样式不该被片段样式「顺带」注入；实际 CSS:\n%s", css)
	}
}

// TestFragmentBaseStylePerCapability 按片段能力分族：只带引用到的那一族。
func TestFragmentBaseStylePerCapability(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		want    []string
		notWant []string
	}{
		{"只有购物车", "<div hx-get='/_fragments/cartView' hx-trigger='load'></div>", []string{".sky-cart-item {"}, []string{".sky-orders-item {"}},
		{"只有订单", "<div hx-get='/_fragments/ordersList?projectId=p1' hx-trigger='load'></div>", []string{".sky-orders-item {", ".sky-order-line-total {"}, []string{".sky-cart-item {"}},
		{"两者都有", "<div hx-get='/_fragments/cartView'></div><div hx-get='/_fragments/orderDetail'></div>", []string{".sky-cart-item {", ".sky-orders-item {"}, nil},
		{"退货也在订单族", "<form hx-post='/_fragments/returnRequest'></form>", []string{".sky-return-form {"}, []string{".sky-cart-item {"}},
		{"结算在购物车族", "<form hx-post='/_fragments/checkout'></form>", []string{".sky-cart-checkout {"}, []string{".sky-orders-item {"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			css := fragmentCSSFor(t, tc.html)
			for _, want := range tc.want {
				if !strings.Contains(css, want) {
					t.Errorf("缺少 %q\n%s", want, css)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(css, notWant) {
					t.Errorf("不该出现 %q（按能力细分失效）\n%s", notWant, css)
				}
			}
		})
	}
}

// TestFragmentBaseStyleNotInjectedWithoutFragmentReference 指向别处的 htmx 请求不带片段样式。
func TestFragmentBaseStyleNotInjectedWithoutFragmentReference(t *testing.T) {
	for _, html := range []string{
		"<div hx-get='/api/products?q=1'></div>",
		"<form hx-post='/cart'></form>",
		"<div data-fragments='/_fragments/cartView'></div>", // 非 htmx 属性：不是消费入口
		"<script>var s = '/_fragments/cartView';</script>",  // 脚本里的字符串不算引用
	} {
		css := fragmentCSSFor(t, html)
		if strings.Contains(css, ".sky-cart-item {") {
			t.Errorf("输入 %s 不该注入片段样式；实际 CSS:\n%s", html, css)
		}
	}
}

// TestFragmentBaseStyleOnlyOnceInProduct 产物级幂等：多个订单组件同页，片段样式只出一份。
func TestFragmentBaseStyleOnlyOnceInProduct(t *testing.T) {
	set := crosscheckSet(t)
	cp := crosscheckCompile(t, set, []*core.Node{
		probeNode("ol-a", "core.orderList", "{}"),
		probeNode("ol-b", "core.orderList", "{}"),
	}, WithProjectID("proj-fragment"))
	doc := renderOrFatal(t, cp)
	if n := strings.Count(doc, ".sky-orders-item {"); n != 1 {
		t.Fatalf("订单片段样式在产物里出现 %d 次（应为 1 次）", n)
	}
	if n := strings.Count(doc, ".sky-cart-item {"); n != 0 {
		t.Fatalf("页面没有引用购物车片段，却带了 %d 份购物车片段样式", n)
	}
}

// TestFragmentBaseStyleInRealProductDocument 产物级：写了 hx-post 就拿到样式（不经组件登记）。
func TestFragmentBaseStyleInRealProductDocument(t *testing.T) {
	doc, err := RenderDocument(&CompiledPage{
		HTML:     "<button type='button' hx-post='/_fragments/cartAdd'>加购</button>",
		UIStyle:  "/* ui css */",
		Features: core.NewFeatureSet(),
	})
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if !strings.Contains(doc, ".sky-cart-item {") {
		t.Fatalf("自定义入口的页面没拿到购物车片段样式；产物：\n%s", doc)
	}
}

// TestFragmentCapabilityTablesCoverEachOther 两张表（有样式族 / 无样式族）互相闭合。
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
	if len(styled) < 8 {
		t.Fatalf("有样式族只登记了 %d 个能力（预期 ≥8）：表可能被清空", len(styled))
	}
	for name, reason := range unstyled {
		if len([]rune(strings.TrimSpace(reason))) < 12 {
			t.Errorf("无样式能力 %s 缺少现状说明（每个都要写清「为什么还没有样式 / 归属在哪」）", name)
		}
	}
}

// TestFragmentBaseCSSObeysBaseRules 片段基座样式自身要守两条基座规则：
// 只消费 --sky-* 令牌（审计 UIK-003 第一层），且不引入节点作用域类。
func TestFragmentBaseCSSObeysBaseRules(t *testing.T) {
	var all strings.Builder
	for _, g := range fragmentStyleGroups() {
		all.WriteString(g.css)
		all.WriteString("\n")
	}
	css := all.String()
	if strings.Contains(css, "var(--c-") {
		t.Error("片段基座引用了后台令牌 --c-*：基座只写 --sky-*（后台取值由 theme.css 的别名段供给）")
	}
	if !strings.Contains(css, "min-width: 0") {
		t.Error("片段基座缺少 min-width: 0：窄屏下 flex 子项会被内容撑宽（多端硬规则）")
	}
	if strings.Contains(css, ".sky-c-") {
		t.Error("片段基座里出现节点作用域类 .sky-c-*：片段是运行时 HTML，拿不到构建期的 node id")
	}
}

// TestFragmentBaseCSSPassesMultiDeviceGuard 片段基座样式必须自己过一遍多端硬规则守卫。
//
// 守卫扫的是组件源与后台静态样式（css_verify_scan.go），基座样式是 Go 里的常量，
// 不在那两条扫描路径上 —— 少了这道断言，基座就成了守卫的盲区（UI-015 想防的正是这个）。
func TestFragmentBaseCSSPassesMultiDeviceGuard(t *testing.T) {
	for _, g := range fragmentStyleGroups() {
		if v := analyzeCSS(g.css, "fragment-base:"+g.id, "", false); len(v) > 0 {
			for _, one := range v {
				t.Errorf("片段基座族 %s 违反多端硬规则：%s", g.id, one)
			}
		}
	}
}
