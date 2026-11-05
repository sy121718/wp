package productselector

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func productSelectorCSSFor(t *testing.T, p *Props) string {
	t.Helper()
	var b core.CSSBuckets
	CompileCSS("t", p, &b)
	return b.String()
}

func TestProductSelectorCSSRules(t *testing.T) {
	out := productSelectorCSSFor(t, &Props{})
	want := []string{
		".sky-c-t {\n  display: flex;",
		".sky-c-t .sky-selector-group {",
		".sky-c-t .sky-selector-legend {",
		".sky-c-t .sky-selector-values {",
		".sky-c-t .sky-selector-radio {",
		".sky-c-t .sky-selector-value {",
		".sky-c-t .sky-selector-radio:checked + .sky-selector-value {",
		".sky-c-t .sky-selector-radio:focus-visible + .sky-selector-value {",
		".sky-c-t .sky-selector-variants {",
		".sky-c-t .sky-selector-variant {",
		".sky-c-t .sky-selector-variant-price {",
		".sky-c-t .sky-selector-variant-compare {",
		".sky-c-t .sky-selector-live {",
		".sky-c-t .sky-selector-stock {",
		".sky-c-t .sky-selector-variant .is-out {",
		".sky-c-t .sky-selector-empty {",
	}
	last := -1
	for _, w := range want {
		i := strings.Index(out, w)
		if i < 0 {
			t.Fatalf("规则缺失: %q\n%s", w, out)
		}
		if i < last {
			t.Errorf("规则顺序漂移: %q\n%s", w, out)
		}
		last = i
	}

	if n := strings.Count(out, " {"); n != len(want) {
		t.Errorf("规则条数应为 %d，实际 %d:\n%s", len(want), n, out)
	}
}

func TestProductSelectorCSSScope(t *testing.T) {
	out := productSelectorCSSFor(t, &Props{})
	for _, bad := range []string{".sky-c-t.sky-c-t", ".sky-c-t .sky-c-t"} {
		if strings.Contains(out, bad) {
			t.Errorf("选择器里多拼了作用域前缀 %q:\n%s", bad, out)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "&") {
		t.Errorf("产物里有未展开的占位或未替换的作用域前缀:\n%s", out)
	}
	if strings.Contains(out, "@media") || strings.Contains(out, "@container") {
		t.Errorf("规格选择器没有断点 / 容器档位，不该产出媒体查询:\n%s", out)
	}
}

func TestProductSelectorCSSKeyboardPath(t *testing.T) {
	out := productSelectorCSSFor(t, &Props{})
	radio := out[strings.Index(out, ".sky-c-t .sky-selector-radio {"):]
	radio = radio[:strings.Index(radio, "}")]
	if strings.Contains(radio, "display: none") {
		t.Errorf("radio 不得用 display: none 隐藏（键盘路径会一起消失）:\n%s", radio)
	}
	for _, want := range []string{"position: absolute;", "clip-path: inset(50%);"} {
		if !strings.Contains(radio, want) {
			t.Errorf("radio 隐藏方式缺少 %q:\n%s", want, radio)
		}
	}
	if !strings.Contains(out, ".sky-c-t .sky-selector-radio:focus-visible + .sky-selector-value {\n  outline: 2px solid var(--sky-c-primary, #2563eb);\n  outline-offset: 2px;\n}") {
		t.Errorf("焦点环规则缺失或丢了 outline-offset:\n%s", out)
	}

	if !strings.Contains(out, ":checked + .sky-selector-value") {
		t.Errorf("选中态应走兄弟选择器:\n%s", out)
	}
}

func TestProductSelectorCSSIndependentOfProps(t *testing.T) {
	base := productSelectorCSSFor(t, &Props{})
	for _, p := range []*Props{{Stock: "off"}, {EmptyText: "暂无"}, {Currency: "USD"}, {Stock: ""}} {
		if got := productSelectorCSSFor(t, p); got != base {
			t.Errorf("样式不该随 Props 变化（props=%+v）:\n%s", p, got)
		}
	}
}
