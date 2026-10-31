package counter

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func TestCounterCSSCounterMode(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{End: 100}, &b)
	out := b.String()
	for _, want := range []string{
		"@keyframes sky-counter-run {",
		"from { --sky-count: var(--sky-counter-from) }",
		"content: counter(wpcount);",
		"animation-timeline: view();",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}

	if strings.Contains(out, "@property") {
		t.Errorf("@property 不该混进基础样式:\n%s", out)
	}
	if !strings.Contains(b.TopLevelCSS(), "@property --sky-count {\n  syntax: \"<integer>\"\n  initial-value: 0\n  inherits: false\n}") {
		t.Errorf("@property 注册块缺失或形态不符:\n%s", b.TopLevelCSS())
	}

	var d core.CSSBuckets
	compileCSS("t", &Props{End: 99.5, Decimals: 2}, &d)
	if strings.Contains(d.String(), "sky-counter-run") || d.TopLevelCSS() != "" {
		t.Errorf("小数位模式不该产出 CSS 计数三件套:\n%s\n%s", d.String(), d.TopLevelCSS())
	}
}

func TestCounterCSSValues(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{End: 1}, &b)
	out := b.String()
	for _, want := range []string{"justify-content: center;", "font-size: 2rem;", "color: inherit;"} {
		if !strings.Contains(out, want) {
			t.Errorf("缺省值 %q 缺失:\n%s", want, out)
		}
	}

	var c core.CSSBuckets
	compileCSS("t", &Props{Start: 10, End: 500, Align: "left", FontSize: "3rem", Color: "#f00", Duration: 4}, &c)
	styled := c.String()
	for _, want := range []string{"justify-content: left;", "font-size: 3rem;", "color: #f00;", "--sky-counter-from: 10;", "--sky-counter-to: 500;", "--sky-counter-duration: 4s;"} {
		if !strings.Contains(styled, want) {
			t.Errorf("产物缺少 %q\n%s", want, styled)
		}
	}
}

func TestCounterCSSLabelSelector(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	want := "div.sky-c-t-label.sky-counter-label, .sky-counter-label {"
	if !strings.Contains(b.String(), want) {
		t.Errorf("标签规则的选择器形态不符，缺少 %q\n%s", want, b.String())
	}
}
