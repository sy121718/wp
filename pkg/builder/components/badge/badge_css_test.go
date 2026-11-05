package badge

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func cssFor(t *testing.T, p *Props) string {
	t.Helper()
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

func TestBadgeCSSVariants(t *testing.T) {
	base := []string{"display: inline-flex", "padding: var(--sky-space-1, 2px) var(--sky-space-5, 10px)", "border-radius: var(--sky-radius-pill, 9999px)", "white-space: nowrap"}
	const token = "background: var(--sky-c-primary, #2563eb)"
	cases := []struct {
		name    string
		variant string
		color   string
		want    []string
		notWant []string
	}{
		{"solid 缺省色走主题令牌", "", "", []string{token, "color: var(--sky-c-surface, #fff)"}, []string{"border: 1px solid", "color-mix"}},
		{"outline 自定义色", VariantOutline, "#ff0000", []string{"color: #ff0000", "background: transparent", "border: 1px solid #ff0000"}, []string{"color: var(--sky-c-surface, #fff)", "color-mix"}},
		{"soft 自定义色", VariantSoft, "#0f0", []string{"color: #0f0", "background: color-mix(in srgb, #0f0 12%, transparent)"}, []string{"color: var(--sky-c-surface, #fff)", "border: 1px solid"}},
		{"未知变体兜底为实心", "weird", "", []string{token, "color: var(--sky-c-surface, #fff)"}, []string{"border: 1px solid", "color-mix"}},
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
