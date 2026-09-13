package productselector

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func productSelectorCSSFor(t *testing.T, p *Props) string {
	t.Helper()
	var b core.CSSBuckets
	CompileCSS("t", p, &b)
	return b.String()
}

// TestProductSelectorCSSRules 逐条核对 16 条规则与它们的顺序。
//
// 产物的字节包含声明顺序，顺序漂移在页面上表现为「同优先级的规则谁赢变了」——
// 合法 CSS、浏览器不报错，只能在这里钉住。选择器一律带 " {"，避免
// ".sky-selector-variant" 这样的前缀误命中 ".sky-selector-variants"。
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
	// 规则条数固定：多一条少一条都说明样式源被改坏（或选择器被拆成了两条）。
	if n := strings.Count(out, " {"); n != len(want) {
		t.Errorf("规则条数应为 %d，实际 %d:\n%s", len(want), n, out)
	}
}

// TestProductSelectorCSSScope 作用域前缀只拼一次，且产物里不留占位。
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

// TestProductSelectorCSSKeyboardPath 键盘路径不得被删掉：radio 视觉隐藏要用
// position + clip-path 而不是 display: none（后者会把 Tab / 方向键一起删掉），
// 且必须留可见的焦点环。
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
	// 选中态与焦点环都靠**兄弟**选择器（radio 在 label 之前），零 JS。
	if !strings.Contains(out, ":checked + .sky-selector-value") {
		t.Errorf("选中态应走兄弟选择器:\n%s", out)
	}
}

// TestProductSelectorCSSIndependentOfProps 样式不随 Props 变化 —— compileCSS 读不到
// 任何属性（开关只影响模板）。哪天有人在样式里接上条件，这条会立刻失败，
// 提醒迁移者把新增的分支也搬进样式源并更新基线。
func TestProductSelectorCSSIndependentOfProps(t *testing.T) {
	base := productSelectorCSSFor(t, &Props{})
	for _, p := range []*Props{{Stock: "off"}, {EmptyText: "暂无"}, {Currency: "USD"}, {Stock: ""}} {
		if got := productSelectorCSSFor(t, p); got != base {
			t.Errorf("样式不该随 Props 变化（props=%+v）:\n%s", p, got)
		}
	}
}
