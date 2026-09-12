package badge

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// cssFor 用给定 props 生成产物 CSS（node id 固定，便于断言）。
func cssFor(t *testing.T, p *Props) string {
	t.Helper()
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// TestBadgeCSSVariants 三种外观范式各自只产出自己的声明组，且合并进同一条规则。
//
// 三种变体在 badge.css 里是同一个声明块内的三段条件，命中的那一支与基础声明必须合成
// 一条规则 —— 否则产物会多出「同选择器、属性分散」的多条规则（语义等价但字节不同）。
func TestBadgeCSSVariants(t *testing.T) {
	base := []string{"display: inline-flex", "padding: 2px 10px", "border-radius: 9999px", "white-space: nowrap"}
	const token = "background: var(--sky-c-primary, #2563eb)"
	cases := []struct {
		name    string
		variant string
		color   string
		want    []string
		notWant []string
	}{
		{"solid 缺省色走主题令牌", "", "", []string{token, "color: #fff"}, []string{"border: 1px solid", "color-mix"}},
		{"outline 自定义色", VariantOutline, "#ff0000", []string{"color: #ff0000", "background: transparent", "border: 1px solid #ff0000"}, []string{"color: #fff", "color-mix"}},
		{"soft 自定义色", VariantSoft, "#0f0", []string{"color: #0f0", "background: color-mix(in srgb, #0f0 12%, transparent)"}, []string{"color: #fff", "border: 1px solid"}},
		{"未知变体兜底为实心", "weird", "", []string{token, "color: #fff"}, []string{"border: 1px solid", "color-mix"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := cssFor(t, &Props{Text: "新", Variant: c.variant, Color: c.color})
			for _, w := range append(base, c.want...) {
				if !strings.Contains(out, w) {
					t.Errorf("产物缺少 %q\n%s", w, out)
				}
			}
			for _, nw := range c.notWant {
				if strings.Contains(out, nw) {
					t.Errorf("不该出现的声明 %q\n%s", nw, out)
				}
			}
			if n := strings.Count(out, ".sky-c-t {"); n != 1 {
				t.Errorf("badge 应只产出 1 条规则，实际 %d 条:\n%s", n, out)
			}
		})
	}
}
