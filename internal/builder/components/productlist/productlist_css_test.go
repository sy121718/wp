package productlist

// productlist_css_test.go -- productlist.css 的契约测试。
//
// 背景: 样式从 compileCSS 里的 39 处 b.Add 迁成同目录的 productlist.css, 中间多了一层解析。
// 这份样式的易碎点比普通组件多两处:
//   1. 网格列数是「值变量」{{cols}} -- 变量名对不上时声明会整条消失, 页面上只表现为「列数不对」;
//   2. 列表布局独有的两条规则走规则级 @if isList -- 块被截断时后半段规则凭空消失。
// 所以这里把「列数四档 + 两种布局 + 两个变量」逐个钉住。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// listCSSFor 渲染指定 Props 下的样式产物 (作用域固定为 .sky-c-t)。
func listCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// listRuleText 从产物里取出 header 起的一整块 (按花括号配平); 首个匹配即 desktop 段的那条。
func listRuleText(t *testing.T, css, header string) string {
	t.Helper()
	i := strings.Index(css, header)
	if i < 0 {
		t.Fatalf("产物里找不到 %q:\n%s", header, css)
	}
	depth := 0
	for j := i; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[i : j+1]
			}
		}
	}
	t.Fatalf("%q 的花括号没有闭合:\n%s", header, css)
	return ""
}

// TestProductListCSSColumnsPerMode 列声明四档 + 列表布局恒单列 + 非法值回落自适应。
func TestProductListCSSColumnsPerMode(t *testing.T) {
	const auto = "repeat(auto-fill, minmax(min(100%, 15rem), 1fr))"
	cases := []struct {
		layout  string
		columns string
		want    string
	}{
		{"", "", auto},
		{LayoutGrid, ColumnsAuto, auto},
		{LayoutGrid, "2", "repeat(2, minmax(0, 1fr))"},
		{LayoutGrid, "3", "repeat(3, minmax(0, 1fr))"},
		{LayoutGrid, "4", "repeat(4, minmax(0, 1fr))"},
		{LayoutGrid, "7", auto},
		{LayoutList, "", "1fr"},
		{LayoutList, "4", "1fr"},
	}
	for _, c := range cases {
		out := listCSSFor(&Props{Layout: c.layout, Columns: c.columns})
		items := listRuleText(t, out, ".sky-c-t .sky-product-list-items {")
		if !strings.Contains(items, "grid-template-columns: "+c.want+";") {
			t.Errorf("layout=%q columns=%q: 期望 grid-template-columns: %s;, 实际块:\n%s", c.layout, c.columns, c.want, items)
		}
	}
}

// TestProductListCSSEmptyDeclarationsAreNotEmitted 值变量取到空值时不能留下 grid-template-columns: ; 这类无效声明。
func TestProductListCSSEmptyDeclarationsAreNotEmitted(t *testing.T) {
	for _, c := range []struct{ layout, columns string }{
		{"", ""}, {LayoutGrid, ""}, {LayoutList, ""}, {LayoutGrid, "bogus"}, {LayoutGrid, "0"},
	} {
		out := listCSSFor(&Props{Layout: c.layout, Columns: c.columns})
		if strings.Contains(out, "grid-template-columns: ;") || strings.Contains(out, "grid-template-columns:  ;") {
			t.Errorf("layout=%q columns=%q: 留下了无效声明:\n%s", c.layout, c.columns, out)
		}
		if !strings.Contains(out, "grid-template-columns: ") {
			t.Errorf("layout=%q columns=%q: 列声明整条消失 (网格会退化成单列):\n%s", c.layout, c.columns, out)
		}
	}
}

// TestProductListCSSListOnlyRules 列表布局独有的两条规则: 列表模式多一份, 网格模式一条不多。
func TestProductListCSSListOnlyRules(t *testing.T) {
	grid := listCSSFor(&Props{Layout: LayoutGrid, Columns: ColumnsAuto})
	list := listCSSFor(&Props{Layout: LayoutList})

	if strings.Contains(grid, "flex-direction: row") || strings.Contains(grid, "min(100%, 12rem)") {
		t.Errorf("网格模式不该产出列表布局的规则:\n%s", grid)
	}
	if strings.Count(list, "flex-direction: row") != 1 {
		t.Errorf("列表模式应有且仅有一条横排声明:\n%s", list)
	}
	if strings.Count(list, "width: min(100%, 12rem)") != 1 {
		t.Errorf("列表模式的图片固定列缺失或重复:\n%s", list)
	}
	// 列表模式是「基础规则 + 追加两条」, 基础那条不能被吃掉。
	base := listRuleText(t, grid, ".sky-c-t .sky-product-list-item {")
	// 卡片基础规则：竖排 + 浅灰底图区（源站形态）。
	// 卡片本身**没有描边与圆角** —— 一屏几十张卡时描边会把版面割碎，
	// 圆角与浅灰底改由 .sky-product-list-thumb 承担。
	if !strings.Contains(base, "flex-direction: column") || !strings.Contains(base, "background: var(--sky-c-surface, #fff)") {
		t.Errorf("基础卡片规则缺失:\n%s", base)
	}
	// 折扣角标：源站是蓝色胶囊压在图左上角（底色加深一档：白字 12px 需 4.5:1 对比）。
	badge := listRuleText(t, grid, ".sky-c-t .sky-product-list-discount {")
	for _, want := range []string{"position: absolute", "border-radius: 5px", "background: var(--sky-c-primary-deep, #1d4ed8)", "color: #fff", "font-weight: 600"} {
		if !strings.Contains(badge, want) {
			t.Errorf("折扣角标规则缺少 %q:\n%s", want, badge)
		}
	}
	// 图区：定位基准 + 浅灰底（角标挂它上面）。
	thumb := listRuleText(t, grid, ".sky-c-t .sky-product-list-thumb {")
	if !strings.Contains(thumb, "position: relative") || !strings.Contains(thumb, "background: var(--sky-c-bg-soft, #f4f5f6)") {
		t.Errorf("图区规则缺少定位基准或浅灰底:\n%s", thumb)
	}
	if listCount, gridCount := strings.Count(list, ".sky-c-t .sky-product-list-item {"), strings.Count(grid, ".sky-c-t .sky-product-list-item {"); listCount != gridCount+1 {
		t.Errorf("列表模式应比网格模式多一条 item 规则 (list=%d grid=%d)", listCount, gridCount)
	}
	// 图片区同理: 网格一条 (基础), 列表两条 (固定列 + 基础)。
	mediaBase := listRuleText(t, grid, ".sky-c-t .sky-product-list-media {")
	if !strings.Contains(mediaBase, "display: block") {
		t.Errorf("图片区基础规则缺失:\n%s", mediaBase)
	}
	if strings.Count(grid, "background: var(--sky-c-surface-alt, rgba(0,0,0,0.03))") != 1 ||
		strings.Count(list, "background: var(--sky-c-surface-alt, rgba(0,0,0,0.03))") != 1 {
		t.Errorf("两种布局都必须保留图片区基础规则")
	}
	if listCount, gridCount := strings.Count(list, ".sky-c-t .sky-product-list-media {"), strings.Count(grid, ".sky-c-t .sky-product-list-media {"); listCount != gridCount+1 {
		t.Errorf("列表模式应比网格模式多一条图片区规则 (list=%d grid=%d)", listCount, gridCount)
	}
}

// TestProductListCSSMobileSingleColumn 窄视口恒单列: 列数与横排都要被覆盖。
func TestProductListCSSMobileSingleColumn(t *testing.T) {
	grid := listCSSFor(&Props{Layout: LayoutGrid, Columns: "4"})
	mobile := listRuleText(t, grid, "@media (max-width: 767px) {")
	if !strings.Contains(mobile, ".sky-c-t .sky-product-list-items {") || !strings.Contains(mobile, "grid-template-columns: 1fr;") {
		t.Errorf("窄视口单列规则不在 mobile 桶:\n%s", mobile)
	}
	if !strings.Contains(mobile, ".sky-c-t .sky-product-list-layout {") || !strings.Contains(mobile, "flex-direction: column;") {
		t.Errorf("窄视口下筛选栏没有折到上方:\n%s", mobile)
	}
	// 四列模式: 1fr 只该出现在 mobile (宽视口是四列)。
	if strings.Count(grid, "grid-template-columns: 1fr;") != 1 {
		t.Errorf("1fr 应只出现一次 (mobile 覆盖), 实际:\n%s", grid)
	}
	// 列表模式: 宽视口本身就是 1fr, 于是两份 (desktop + mobile 覆盖)。
	if list := listCSSFor(&Props{Layout: LayoutList}); strings.Count(list, "grid-template-columns: 1fr;") != 2 {
		t.Errorf("列表模式期望 desktop + mobile 各一条 1fr:\n%s", list)
	}
}

// TestProductListCSSTouchGovernance 悬浮走 (hover: hover); 触屏的等价形态是按压反馈 (不包媒体查询)。
func TestProductListCSSTouchGovernance(t *testing.T) {
	out := listCSSFor(&Props{})
	hover := listRuleText(t, out, "@media (hover: hover) {")
	if !strings.Contains(hover, ".sky-c-t .sky-product-list-item {") || !strings.Contains(hover, "box-shadow: 0 6px 18px rgba(0,0,0,.08)") {
		t.Errorf("卡片悬停抬升未进 (hover: hover) 桶:\n%s", hover)
	}
	rest := out[strings.LastIndex(out, hover)+len(hover):]
	if !strings.Contains(rest, "box-shadow: none;") {
		t.Errorf("按压反馈缺失 (触屏上卡片会一直保持抬升态):\n%s", rest)
	}
	if strings.Contains(rest, "@media") {
		t.Errorf("按压反馈被包进了媒体查询 (触屏上会失效):\n%s", rest)
	}
	if strings.Contains(out, "(hover: none)") {
		t.Errorf("触屏等价形态由 @active 承担, 不该另出 (hover: none) 块:\n%s", out)
	}
}

// TestProductListCSSScopeIsScoped 作用域替换必须生效, 且不能多拼一次前缀。
func TestProductListCSSScopeIsScoped(t *testing.T) {
	for _, p := range []*Props{{}, {Layout: LayoutList, Columns: "3"}} {
		out := listCSSFor(p)
		if strings.Contains(out, "&") {
			t.Errorf("产物里残留未替换的 &: %s", out)
		}
		if strings.Contains(out, ".sky-c-t .sky-c-t") {
			t.Errorf("作用域前缀被拼了两次: %s", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "@") || !strings.HasSuffix(line, "{") {
				continue
			}
			if !strings.HasPrefix(line, ".sky-c-t") {
				t.Errorf("规则 %q 未带实例作用域前缀 (样式会泄漏到全站)", line)
			}
		}
	}
}

// TestProductListCSSTmplVariableContract 样式源与 Go 侧的变量表必须双向对齐。
//
// 这是「Go 与 .css 各写一半」的接口: 引用了没提供的变量会留下 {{...}} (声明整条消失),
// 提供了没被消费的变量说明 Go 侧多算了一份东西 -- 两种都必须在构建期报错, 而不是静默少样式。
func TestProductListCSSTmplVariableContract(t *testing.T) {
	bad := []struct {
		name string
		vars map[string]string
	}{
		{"缺 cols", map[string]string{"isList": "1"}},
		{"缺 isList", map[string]string{"cols": "1fr"}},
		{"两个都缺", map[string]string{}},
		{"多提供变量", map[string]string{"cols": "1fr", "isList": "", "extra": "1"}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			var b core.CSSBuckets
			if err := core.ApplyComponentCSSTmpl(&b, ".x", productListCSS, c.vars); err == nil {
				t.Errorf("应当报错但通过了: vars=%v", c.vars)
			}
		})
	}
	var b core.CSSBuckets
	if err := core.ApplyComponentCSSTmpl(&b, ".x", productListCSS, map[string]string{"cols": "1fr", "isList": ""}); err != nil {
		t.Errorf("两个变量齐备时不该报错: %v", err)
	}
}
