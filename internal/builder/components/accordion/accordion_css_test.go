package accordion

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func accordionCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// TestAccordionCSSOptionalHeadRules 三个可选声明追加在标题规则末尾。
//
// 顺序也是产物的一部分：迁移前它们在 headRules 末尾被 append，值缺失就整条省略。
func TestAccordionCSSOptionalHeadRules(t *testing.T) {
	plain := accordionCSSFor(&Props{})
	for _, notWant := range []string{"#fafafa", "flex-end", "font-size: 18px"} {
		if strings.Contains(plain, notWant) {
			t.Errorf("未设属性时不该出现 %q:\n%s", notWant, plain)
		}
	}

	styled := accordionCSSFor(&Props{BgColor: "#fafafa", TitleAlign: "right", TitleSize: "18px"})
	head := ".sky-c-t .sky-accordion-head {"
	idx := strings.Index(styled, head)
	if idx < 0 {
		t.Fatalf("标题规则缺失:\n%s", styled)
	}
	rule := styled[idx:]
	bg := strings.Index(rule, "background: #fafafa;")
	jc := strings.Index(rule, "justify-content: flex-end;")
	fs := strings.Index(rule, "font-size: 18px;")
	if bg < 0 || jc < 0 || fs < 0 {
		t.Errorf("三个可选声明应全部产出（right 要翻成 flex-end）:\n%s", rule)
	}
	if !(bg < jc && jc < fs) {
		t.Errorf("可选声明的追加顺序应为 底色 → 对齐 → 字号，实际下标 %d/%d/%d:\n%s", bg, jc, fs, rule)
	}
}

// TestAccordionCSSBorderless 无边框模式是三整条规则的开关。
func TestAccordionCSSBorderless(t *testing.T) {
	plain := accordionCSSFor(&Props{})
	if strings.Contains(plain, "borderless") {
		t.Errorf("未开无边框模式时不该产出相关规则:\n%s", plain)
	}
	full := accordionCSSFor(&Props{Borderless: true})
	for _, want := range []string{
		".sky-c-t.sky-accordion-borderless {\n  gap: 0;",
		".sky-c-t.sky-accordion-borderless .sky-accordion-head {",
		".sky-c-t.sky-accordion-borderless .sky-accordion-body {",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("产物缺少 %q\n%s", want, full)
		}
	}
}

// TestAccordionCSSZeroJS 零 JS 展开：箭头靠伪元素，展开态靠 [open]。
func TestAccordionCSSZeroJS(t *testing.T) {
	out := accordionCSSFor(&Props{})
	for _, want := range []string{
		"content: '＋';",
		".sky-c-t .sky-accordion-head::-webkit-details-marker {",
		".sky-c-t details[open] .sky-c-t .sky-accordion-head::after {\n  transform: rotate(45deg);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	// 悬停底色走 hover 桶：触屏上整段不输出（展开靠点击，不构成缺口）。
	if !strings.Contains(out, "@media (hover: hover) {") {
		t.Errorf("标题悬停未进 (hover: hover) 桶:\n%s", out)
	}
}
