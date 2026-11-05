package product

import (
	"os"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func productCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

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

func TestProductCSSKeepsStaticRules(t *testing.T) {
	out := productCSSFor(&Props{TitleField: "product.name"})
	cases := []struct {
		desc string
		sel  string
		want []string
	}{

		{"桌面两栏", ".sky-c-t {", []string{"display: grid", "grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr)", "gap: 40px", "align-items: start"}},
		{"主图", ".sky-c-t .sky-product-media img {", []string{"width: min(100%, 100%)", "height: auto", "border-radius: var(--sky-radius-xl, 12px)"}},
		{"图集", ".sky-c-t .sky-product-gallery {", []string{"minmax(min(100%, 96px), 1fr)", "gap: var(--sky-space-4, 8px)"}},
		{"图集图", ".sky-c-t .sky-product-gallery img {", []string{"aspect-ratio: 1 / 1", "object-fit: cover", "border-radius: var(--sky-radius-lg, 8px)"}},
		{"信息栏", ".sky-c-t .sky-product-info {", []string{"flex-direction: column", "gap: var(--sky-space-5, 10px)", "min-width: 0"}},

		{"标题", ".sky-c-t .sky-product-title {", []string{"font-size: 30px", "font-weight: 600", "line-height: 1.4"}},
		{"副标题", ".sky-c-t .sky-product-subtitle {", []string{"color: var(--sky-c-text-mute, #6b7280)", "line-height: 1.6"}},
		{"价格行", ".sky-c-t .sky-product-price-row {", []string{"align-items: baseline", "flex-wrap: wrap"}},

		{"价格", ".sky-c-t .sky-product-price {", []string{"font-weight: 700", "font-size: 17.5px", "color: var(--sky-c-heading, #000)"}},
		{"划线价", ".sky-c-t .sky-product-compare {", []string{"text-decoration: line-through"}},
		{"描述", ".sky-c-t .sky-product-description {", []string{"line-height: 1.7", "word-break: break-word"}},
		{"可用量", ".sky-c-t .sky-product-variant-stock {", []string{"font-size: .85rem", "flex: 1 0 100%"}},
		{"缺货态", ".sky-c-t .sky-product-variant .is-out {", []string{"color: var(--sky-c-danger, #dc2626)", "font-weight: 600"}},
		{"规格容器", ".sky-c-t .sky-product-options {", []string{"flex-direction: column", "gap: var(--sky-space-6, 12px)"}},
		{"单个规格组", ".sky-c-t .sky-product-option {", []string{"flex-wrap: wrap", "border: 0", "padding: 0"}},
		{"规格组标签", ".sky-c-t .sky-product-option legend {", []string{"padding: 0", "font-size: var(--sky-fs-base, 13px)"}},
		{"选项标签", ".sky-c-t .sky-product-option-value {", []string{"min-height: 36px", "border-radius: var(--sky-radius-lg, 8px)", "cursor: pointer"}},
		{"选中态", ".sky-c-t .sky-product-option-radio:checked + .sky-product-option-value {", []string{"border-color: var(--sky-c-primary, #2563eb)", "background: rgba(37,99,235,0.08)"}},
		{"变体列表", ".sky-c-t .sky-product-variants {", []string{"flex-direction: column", "gap: var(--sky-space-3, 6px)"}},
		{"单个变体", ".sky-c-t .sky-product-variant {", []string{"gap: var(--sky-space-2, 4px) var(--sky-space-5, 10px)", "font-size: var(--sky-fs-base, 13px)"}},
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

func TestProductCSSRadioStaysKeyboardReachable(t *testing.T) {

	jet, err := os.ReadFile("../product/product.jet")
	if err != nil {
		t.Fatalf("读模板失败: %v", err)
	}
	if !strings.Contains(string(jet), "sky-product-option-radio sky-sr-only") {
		t.Errorf("radio 模板缺少 sky-sr-only —— 要么它会显示成原生单选钮，要么被别的写法移出键盘序列")
	}
	out := productCSSFor(&Props{OptionsField: "product.options"})

	if strings.Contains(out, ".sky-c-t .sky-product-option-radio {\n  display: none") {
		t.Errorf("radio 用 display:none 隐藏会让整组离开键盘序列:\n%s", out)
	}
	focus := cssRuleText(t, out, ".sky-c-t .sky-product-option-radio:focus-visible + .sky-product-option-value {")
	if !strings.Contains(focus, "outline: 2px solid var(--sky-c-primary, #2563eb)") || !strings.Contains(focus, "outline-offset: 2px") {
		t.Errorf("键盘焦点环缺失 (焦点环要画在 label 上):\n%s", focus)
	}
}

func TestProductCSSMobileStackAndTapTarget(t *testing.T) {
	out := productCSSFor(&Props{})
	mobile := cssRuleText(t, out, "@media (max-width: 767px) {")
	if !strings.Contains(mobile, ".sky-c-t {") || !strings.Contains(mobile, "flex-direction: column") {
		t.Errorf("窄视口纵向堆叠规则不在 mobile 桶:\n%s", mobile)
	}
	if !strings.Contains(mobile, ".sky-c-t .sky-product-option-value {") || !strings.Contains(mobile, "min-height: 44px") {
		t.Errorf("窄视口按压目标 (44px) 不在 mobile 桶:\n%s", mobile)
	}

	if strings.Count(out, "min-height: 44px") != 1 {
		t.Errorf("min-height: 44px 应只出现一次 (mobile 桶):\n%s", out)
	}
	if !strings.Contains(out, "min-height: 36px") {
		t.Errorf("桌面档的 36px 目标高被覆盖掉了:\n%s", out)
	}
}

func TestProductCSSTouchGovernance(t *testing.T) {
	out := productCSSFor(&Props{})
	hover := cssRuleText(t, out, "@media (hover: hover) {")
	if !strings.Contains(hover, ".sky-c-t .sky-product-option-value {") || !strings.Contains(hover, "border-color: var(--sky-c-primary, #2563eb)") {
		t.Errorf("选项标签的悬停反馈未进 (hover: hover) 桶:\n%s", hover)
	}
	if strings.Contains(hover, "translateY") {
		t.Errorf("按压位移混进了悬浮桶 (触屏会整段不输出, 等于没有按下反馈):\n%s", hover)
	}

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
