package orderlist

// orderlist_test.go — 订单列表组件的视图测试。
//
// 钉住的是**不变量**：
//   · 片段地址必须带工程 id（不带就永远拉不到订单，且不会有任何报错）；
//   · 每页条数必须带上（否则组件上改了它、片段按默认值取，表现是「设置不生效」）；
//   · 槽位没配时不输出链接（不猜路径 —— 猜错的链接比没有链接难查）；
//   · 缺工程 id 时给可见提示，而不是渲染一个永远空着的容器。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestBuildViewCarriesProjectLangAndPageSize 片段地址的三个要素：工程、语言、每页条数。
func TestBuildViewCarriesProjectLangAndPageSize(t *testing.T) {
	v := BuildView(&Props{PageSize: 25}, "proj-9", "en-US", "/login", "/orders")
	if !strings.HasPrefix(v.FragmentURL, ordersListPath) {
		t.Fatalf("片段路径不对: %q", v.FragmentURL)
	}
	for _, want := range []string{"projectId=proj-9", "lang=en-US", "limit=25"} {
		if !strings.Contains(v.FragmentURL, want) {
			t.Fatalf("片段地址缺少 %q: %q", want, v.FragmentURL)
		}
	}
}

// TestBuildViewOmitsEmptyLang 语言为空时不带 lang 参数（单语言站点不做无用判断）。
func TestBuildViewOmitsEmptyLang(t *testing.T) {
	v := BuildView(&Props{}, "proj-9", "  ", "", "")
	if strings.Contains(v.FragmentURL, "lang=") {
		t.Fatalf("语言为空时不该带 lang 参数: %q", v.FragmentURL)
	}
	// 每页条数即使作者没填也要带上：片段侧的默认值与组件侧的默认值
	// 是两个常量，靠「两边都写成 10」来对齐是迟早要出错的。
	if !strings.Contains(v.FragmentURL, "limit=") {
		t.Fatalf("缺省每页条数也必须显式传给片段: %q", v.FragmentURL)
	}
}

// TestBuildViewWithoutProjectRendersNotice 缺工程 id 时给可见提示，不让整页构建失败。
//
// pipeline.DefaultCompile 明确不带工程 id（见该函数注释），把它当致命错误
// 会让「一个区块配不出工程，整页发布不了」。
func TestBuildViewWithoutProjectRendersNotice(t *testing.T) {
	for _, pid := range []string{"", "   "} {
		v := BuildView(&Props{}, pid, "zh-CN", "/login", "/orders")
		if v.Notice == "" {
			t.Fatal("缺工程 id 时应给可见提示，而不是渲染一个永远空着的容器")
		}
		if v.FragmentURL != "" {
			t.Fatalf("不可用时不该输出片段地址: %q", v.FragmentURL)
		}
	}
}

// TestBuildViewPageSizeClamped 每页条数越界回落（与片段侧同一口径）。
func TestBuildViewPageSizeClamped(t *testing.T) {
	for _, tt := range []struct {
		in   int
		want int
	}{
		{0, defaultPageSize},
		{-3, defaultPageSize},
		{1, 1},
		{50, 50},
		{999, 50},
	} {
		if got := effectivePageSize(&Props{PageSize: tt.in}); got != tt.want {
			t.Errorf("effectivePageSize(%d)=%d, want=%d", tt.in, got, tt.want)
		}
	}
}

// TestBuildViewSlotLinks 槽位链接：配了才有，没配不输出（不猜路径）。
func TestBuildViewSlotLinks(t *testing.T) {
	v := BuildView(&Props{}, "proj-1", "zh-CN", "", "")
	if v.HasLoginURL || v.HasOrderPage {
		t.Fatal("槽位没配时不该有链接")
	}
	v2 := BuildView(&Props{}, "proj-1", "zh-CN", "  /login  ", " /orders ")
	if !v2.HasLoginURL || v2.LoginURL != "/login" {
		t.Fatalf("登录链接应去空白后使用，实际 %q (has=%v)", v2.LoginURL, v2.HasLoginURL)
	}
	if !v2.HasOrderPage || v2.OrderPageURL != "/orders" {
		t.Fatalf("订单页链接应去空白后使用，实际 %q (has=%v)", v2.OrderPageURL, v2.HasOrderPage)
	}
}

// TestEffectiveTitle 标题缺省。
func TestEffectiveTitle(t *testing.T) {
	if got := effectiveTitle(&Props{}); got != defaultTitle {
		t.Fatalf("空标题应回退默认值，实际 %q", got)
	}
	if got := effectiveTitle(&Props{Title: "   "}); got != defaultTitle {
		t.Fatalf("纯空白应回退默认值，实际 %q", got)
	}
	if got := effectiveTitle(&Props{Title: "我的订单记录"}); got != "我的订单记录" {
		t.Fatalf("自定义标题应原样使用，实际 %q", got)
	}
}

// TestCompileCSSLeavesFragmentStylesToBase 片段基座样式**不由组件注入**（审计 UIK-005）。
//
// 迁移前片段样式由组件带出，后果是「页面上放没放这个组件」决定片段有没有样式：
// 只写 hx-post="/_fragments/cartAdd" 的自定义入口拿到的是裸 HTML（条目原文的症状）。
// 现在片段样式归基座（internal/builder/fragment_base.go），按 hx-* 指向 /_fragments/ 判定，
// 组件只负责自己的节点作用域外壳。本用例反向钉住「别把片段样式又写回组件」——
// 写回去就等于把那个耦合重新装回来。
//
// 片段样式本身的两条验收（自定义引用也能拿到、按片段能力细分）在 builder 包：
// internal/builder/fragment_base_test.go。
func TestCompileCSSLeavesFragmentStylesToBase(t *testing.T) {
	var b core.CSSBuckets
	CompileCSS("t", &Props{Color: "#c00"}, &b)
	css := b.String()
	for _, unwanted := range []string{
		".sky-orders-list", ".sky-orders-item", ".sky-order-line-total", ".sky-order-logs",
	} {
		if strings.Contains(css, unwanted) {
			t.Fatalf("组件样式里出现片段基座类 %q：片段样式只该在基座里定义（UIK-005）；实际样式：\n%s", unwanted, css)
		}
	}
	if !strings.Contains(css, "#c00") {
		t.Fatalf("自定义强调色应进产物；实际样式：\n%s", css)
	}
	if !strings.Contains(css, ".sky-c-t") {
		t.Fatalf("组件外壳样式必须按 node id 作用域化；实际样式：\n%s", css)
	}
}

// TestCompileCSSScopedPerInstance 作用域按 node id 派生（同页两个列表互不污染）。
func TestCompileCSSScopedPerInstance(t *testing.T) {
	var b1, b2 core.CSSBuckets
	CompileCSS("listA", &Props{}, &b1)
	CompileCSS("listB", &Props{}, &b2)
	if b1.String() == b2.String() {
		t.Fatal("不同 node id 的样式应各自作用域化")
	}
}
