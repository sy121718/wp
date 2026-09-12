package product

// product_css_test.go -- product.css 的契约测试。
//
// 背景: 样式从 compileCSS 里的 28 处 b.Add 迁成同目录的 product.css, 中间多了一层解析。
// 解析器写错、选择器替换错、@media 块归错桶 -- 任何一处出错都只会表现为「页面上有点不对劲」,
// 不会让构建失败。所以这里把「容易被无声破坏的不变量」逐条钉住。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// productCSSFor 渲染指定 Props 下的样式产物 (作用域固定为 .sky-c-t)。
func productCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// cssRuleText 从产物里取出 header 起的一整块 (按花括号配平)。
func cssRuleText(t *testing.T, css, header string) string {
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

// TestProductCSSKeepsStaticRules 逐条核对静态规则还在 (选择器 + 只属于它的关键声明)。
func TestProductCSSKeepsStaticRules(t *testing.T) {
	out := productCSSFor(&Props{TitleField: "product.name"})
	cases := []struct {
		desc string
		sel  string
		want []string
	}{
		{"桌面两栏", ".sky-c-t {", []string{"display: grid", "grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr)", "gap: 24px", "align-items: start"}},
		{"主图", ".sky-c-t .sky-product-media img {", []string{"width: min(100%, 100%)", "height: auto", "border-radius: 12px"}},
		{"图集", ".sky-c-t .sky-product-gallery {", []string{"minmax(min(100%, 96px), 1fr)", "gap: 8px"}},
		{"图集图", ".sky-c-t .sky-product-gallery img {", []string{"aspect-ratio: 1 / 1", "object-fit: cover", "border-radius: 8px"}},
		{"信息栏", ".sky-c-t .sky-product-info {", []string{"flex-direction: column", "gap: 10px", "min-width: 0"}},
		{"标题", ".sky-c-t .sky-product-title {", []string{"font-size: 1.5rem", "line-height: 1.35"}},
		{"副标题", ".sky-c-t .sky-product-subtitle {", []string{"color: var(--sky-c-muted, rgba(0,0,0,0.6))", "line-height: 1.6"}},
		{"价格行", ".sky-c-t .sky-product-price-row {", []string{"align-items: baseline", "flex-wrap: wrap"}},
		{"价格", ".sky-c-t .sky-product-price {", []string{"font-weight: 700", "color: var(--sky-c-primary, #2563eb)"}},
		{"划线价", ".sky-c-t .sky-product-compare {", []string{"text-decoration: line-through"}},
		{"描述", ".sky-c-t .sky-product-description {", []string{"line-height: 1.7", "word-break: break-word"}},
		{"可用量", ".sky-c-t .sky-product-variant-stock {", []string{"font-size: .85rem", "flex: 1 0 100%"}},
		{"缺货态", ".sky-c-t .sky-product-variant .is-out {", []string{"color: var(--sky-c-danger, #dc2626)", "font-weight: 600"}},
		{"规格容器", ".sky-c-t .sky-product-options {", []string{"flex-direction: column", "gap: 12px"}},
		{"单个规格组", ".sky-c-t .sky-product-option {", []string{"flex-wrap: wrap", "border: 0", "padding: 0"}},
		{"规格组标签", ".sky-c-t .sky-product-option legend {", []string{"padding: 0", "font-size: 13px"}},
		{"选项标签", ".sky-c-t .sky-product-option-value {", []string{"min-height: 36px", "border-radius: 8px", "cursor: pointer"}},
		{"选中态", ".sky-c-t .sky-product-option-radio:checked + .sky-product-option-value {", []string{"border-color: var(--sky-c-primary, #2563eb)", "background: rgba(37,99,235,0.08)"}},
		{"变体列表", ".sky-c-t .sky-product-variants {", []string{"flex-direction: column", "gap: 6px"}},
		{"单个变体", ".sky-c-t .sky-product-variant {", []string{"gap: 4px 10px", "font-size: 13px"}},
		{"变体规格", ".sky-c-t .sky-product-variant-options {", []string{"word-break: break-word"}},
		{"变体价", ".sky-c-t .sky-product-variant-price {", []string{"font-weight: 600"}},
	}
	for _, c := range cases {
		block := cssRuleText(t, out, c.sel)
		for _, w := range c.want {
			if !strings.Contains(block, w) {
				t.Errorf("%s: %s 里缺少 %q\n%s", c.desc, c.sel, w, block)
			}
		}
	}
}

// TestProductCSSRadioStaysKeyboardReachable 隐藏 radio 只能「视觉隐藏」, 不能离开键盘序列。
//
// display:none 会把整组控件从 Tab 序列里摘掉 -- 键盘用户从此选不了规格, 而页面上看不出任何异常。
func TestProductCSSRadioStaysKeyboardReachable(t *testing.T) {
	out := productCSSFor(&Props{OptionsField: "product.options"})
	radio := cssRuleText(t, out, ".sky-c-t .sky-product-option-radio {")
	if !strings.Contains(radio, "clip-path: inset(50%)") {
		t.Errorf("radio 未做视觉隐藏 (clip-path):\n%s", radio)
	}
	if strings.Contains(radio, "display: none") || strings.Contains(radio, "display:none") {
		t.Errorf("radio 用 display:none 隐藏会让整组离开键盘序列:\n%s", radio)
	}
	focus := cssRuleText(t, out, ".sky-c-t .sky-product-option-radio:focus-visible + .sky-product-option-value {")
	if !strings.Contains(focus, "outline: 2px solid var(--sky-c-primary, #2563eb)") || !strings.Contains(focus, "outline-offset: 2px") {
		t.Errorf("键盘焦点环缺失 (焦点环要画在 label 上):\n%s", focus)
	}
}

// TestProductCSSMobileStackAndTapTarget 窄视口两条规则必须落在 mobile 桶, 且各只有一份。
func TestProductCSSMobileStackAndTapTarget(t *testing.T) {
	out := productCSSFor(&Props{})
	mobile := cssRuleText(t, out, "@media (max-width: 767px) {")
	if !strings.Contains(mobile, ".sky-c-t {") || !strings.Contains(mobile, "flex-direction: column") {
		t.Errorf("窄视口纵向堆叠规则不在 mobile 桶:\n%s", mobile)
	}
	if !strings.Contains(mobile, ".sky-c-t .sky-product-option-value {") || !strings.Contains(mobile, "min-height: 44px") {
		t.Errorf("窄视口按压目标 (44px) 不在 mobile 桶:\n%s", mobile)
	}
	// 44px 只能出现在 mobile: 桌面是 36px, 两处都在才算「覆盖」而不是「替换」。
	if strings.Count(out, "min-height: 44px") != 1 {
		t.Errorf("min-height: 44px 应只出现一次 (mobile 桶):\n%s", out)
	}
	if !strings.Contains(out, "min-height: 36px") {
		t.Errorf("桌面档的 36px 目标高被覆盖掉了:\n%s", out)
	}
}

// TestProductCSSTouchGovernance 悬浮走 (hover: hover), 按压反馈不包媒体查询 (触屏唯一可靠的反馈)。
func TestProductCSSTouchGovernance(t *testing.T) {
	out := productCSSFor(&Props{})
	hover := cssRuleText(t, out, "@media (hover: hover) {")
	if !strings.Contains(hover, ".sky-c-t .sky-product-option-value {") || !strings.Contains(hover, "border-color: var(--sky-c-primary, #2563eb)") {
		t.Errorf("选项标签的悬停反馈未进 (hover: hover) 桶:\n%s", hover)
	}
	if strings.Contains(hover, "translateY") {
		t.Errorf("按压位移混进了悬浮桶 (触屏会整段不输出, 等于没有按下反馈):\n%s", hover)
	}
	// 悬浮块之后就是 active 段 (String 的桶序: hover -> active)。
	rest := out[strings.LastIndex(out, hover)+len(hover):]
	if !strings.Contains(rest, "transform: translateY(1px);") {
		t.Errorf("按压反馈缺失:\n%s", rest)
	}
	if strings.Contains(rest, "@media") {
		t.Errorf("按压反馈被包进了媒体查询 (触屏上会失效):\n%s", rest)
	}
	if strings.Contains(out, "(hover: none)") {
		t.Errorf("product 的悬停只是增强 (选中态走 :checked), 不该产出触屏等价 hover 形态:\n%s", out)
	}
}

// TestProductCSSScopeIsScoped 作用域替换必须生效, 且不能多拼一次前缀。
func TestProductCSSScopeIsScoped(t *testing.T) {
	out := productCSSFor(&Props{})
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

// TestProductCSSIsPropsIndependent 样式源是静态的: 不同 Props 产物相同, 且样式源里没有未展开的占位。
func TestProductCSSIsPropsIndependent(t *testing.T) {
	base := productCSSFor(&Props{})
	for _, p := range []*Props{
		{MediaField: "product.defaultImage", GalleryField: "product.images"},
		{Currency: "$", TitleTag: "h1", OptionsField: "product.options"},
		{VariantsField: "product.variants", DescriptionField: "product.description", Source: "product"},
	} {
		if got := productCSSFor(p); got != base {
			t.Errorf("样式与 Props 无关, 产物却不同:\n--- base ---\n%s\n--- got ---\n%s", base, got)
		}
	}
	if strings.Contains(productCSS, "{{") {
		t.Errorf("product 走 ApplyComponentCSS (无变量表), 样式源里不该有 {{...}} 占位")
	}
}
