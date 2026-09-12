package counter

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestCounterCSSCounterMode 整数模式才产出零 JS 计数三件套。
//
// 三件套必须同进同退：@property 注册（类型与初值）、keyframes、以及 ::after 的
// counter() 显示。少任何一件，产物都是一份合法但不动（或显示为空）的 CSS。
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
	// @property 必须落在顶层桶：层内注册会让浏览器对「层内注册」产生实现差异。
	if strings.Contains(out, "@property") {
		t.Errorf("@property 不该混进基础样式:\n%s", out)
	}
	if !strings.Contains(b.TopLevelCSS(), "@property --sky-count {\n  syntax: \"<integer>\"\n  initial-value: 0\n  inherits: false\n}") {
		t.Errorf("@property 注册块缺失或形态不符:\n%s", b.TopLevelCSS())
	}

	// 小数位模式：CSS counter 表达不了，三件套整体不产出。
	var d core.CSSBuckets
	compileCSS("t", &Props{End: 99.5, Decimals: 2}, &d)
	if strings.Contains(d.String(), "sky-counter-run") || d.TopLevelCSS() != "" {
		t.Errorf("小数位模式不该产出 CSS 计数三件套:\n%s\n%s", d.String(), d.TopLevelCSS())
	}
}

// TestCounterCSSValues 对齐 / 字号 / 颜色的取默认与自定义。
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

// TestCounterCSSLabelSelector 标签规则是「带作用域的元素名 + 无作用域兜底」一条规则。
//
// 这两半必须同属一条规则：拆成两条会改变产物字节（字节等价是迁移的硬要求）。
func TestCounterCSSLabelSelector(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{}, &b)
	want := "div.sky-c-t-label.sky-counter-label, .sky-counter-label {"
	if !strings.Contains(b.String(), want) {
		t.Errorf("标签规则的选择器形态不符，缺少 %q\n%s", want, b.String())
	}
}
