package quote

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func TestQuoteCSSRules(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	out := b.String()
	for _, want := range []string{
		".sky-c-t {",
		"padding: var(--sky-space-7, 16px) 20px",
		"border-left: 4px solid var(--sky-c-primary, #2563eb)",
		"font-style: italic",
		".sky-c-t p {",
		"line-height: 1.7",
		".sky-c-t cite {",
		"color: rgba(0,0,0,0.6)",
		".sky-c-t cite a {",
		"text-decoration: none",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if strings.Contains(out, "text-align") {
		t.Errorf("未配置居中却产出了 text-align:\n%s", out)
	}

	var c core.CSSBuckets
	compileCSS("t", &Props{Align: AlignCenter}, &c)
	if !strings.Contains(c.String(), "text-align: center") {
		t.Errorf("居中时缺少 text-align: center:\n%s", c.String())
	}
}
