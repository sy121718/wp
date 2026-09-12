package progress

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestProgressCSSRules 迁移到 progress.css 后宽度百分比与配色逐条核对。
func TestProgressCSSRules(t *testing.T) {
	const token = "background: var(--sky-c-primary, #2563eb)"
	cases := []struct {
		name  string
		p     Props
		width string
		color string
	}{
		{"50/100 缺省色", Props{Value: 50}, "width: 50%", token},
		{"7/10 自定义色", Props{Value: 7, Max: 10, Color: "#abcdef"}, "width: 70%", "background: #abcdef"},
		{"0/100", Props{Value: 0}, "width: 0%", token},
		{"100/100", Props{Value: 100, Max: 100}, "width: 100%", token},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b core.CSSBuckets
			compileCSS("t", &c.p, &b)
			out := b.String()
			for _, want := range []string{
				".sky-c-t {",
				".sky-c-t .sky-progress-track {",
				".sky-c-t .sky-progress-bar {",
				c.width,
				c.color,
				"border-radius: 9999px",
				".sky-c-t .sky-progress-label {",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("产物缺少 %q\n%s", want, out)
				}
			}
		})
	}
}
