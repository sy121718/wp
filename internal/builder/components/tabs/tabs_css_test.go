package tabs

import (
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func tabsCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// itoa 只在拼期望值时用，与实现侧的 strconv.Itoa 保持同一形态。
func itoa(i int) string { return strconv.Itoa(i) }

// TestTabsCSSEachExpansion 每个页签各展开一组面板与高亮规则。
//
// 规则数量随标签数变化，靠样式源的 @each 展开；少一条就是「某个页签点了没反应」。
func TestTabsCSSEachExpansion(t *testing.T) {
	out := tabsCSSFor(&Props{Tabs: []Tab{{Label: "A"}, {Label: "B"}, {Label: "C"}}})
	for i := 0; i < 3; i++ {
		want := ".sky-c-t:has(#sky-tabs-t-" + itoa(i) + ":checked) .sky-tab-panel[data-index=\"" + itoa(i) + "\"] {\n  display: block;"
		if !strings.Contains(out, want) {
			t.Errorf("缺少第 %d 个面板的显隐规则：%q\n%s", i, want, out)
		}
		label := ".sky-c-t:has(#sky-tabs-t-" + itoa(i) + ":checked) .sky-tabs-nav label:nth-of-type(" + itoa(i+1) + ") {"
		if !strings.Contains(out, label) {
			t.Errorf("缺少第 %d 个标签的高亮规则：%q\n%s", i, label, out)
		}
	}
	// 面板选择器不带实例作用域前缀：:has() 已挂在容器上，再拼一次会要求嵌套两层容器。
	if strings.Contains(out, ".sky-c-t:has(#sky-tabs-t-0:checked) .sky-c-t .sky-tab-panel") {
		t.Errorf("面板选择器多拼了一次作用域前缀（永不匹配）:\n%s", out)
	}
}

// TestTabsCSSActiveColor 自定义激活色是「一个开关包一组循环」，未开时整段不产出。
func TestTabsCSSActiveColor(t *testing.T) {
	plain := tabsCSSFor(&Props{Tabs: []Tab{{Label: "A"}, {Label: "B"}}})
	if strings.Contains(plain, "~ .sky-c-t .sky-tabs-nav label") {
		t.Errorf("未设激活色时不该产出自定义规则:\n%s", plain)
	}
	full := tabsCSSFor(&Props{Tabs: []Tab{{Label: "A"}, {Label: "B"}}, ActiveColor: "#f00"})
	for i := 0; i < 2; i++ {
		want := "#sky-tabs-t-" + itoa(i) + ":checked ~ .sky-c-t .sky-tabs-nav label:nth-of-type(" + itoa(i+1) + ") {\n  background: #f00;"
		if !strings.Contains(full, want) {
			t.Errorf("缺少第 %d 条激活色规则：%q\n%s", i, want, full)
		}
	}
}

// TestTabsCSSVerticalAndAlign 竖向布局与对齐映射。
func TestTabsCSSVerticalAndAlign(t *testing.T) {
	plain := tabsCSSFor(&Props{Tabs: []Tab{{Label: "A"}}})
	if !strings.Contains(plain, "justify-content: flex-start;") {
		t.Errorf("默认左对齐缺失:\n%s", plain)
	}
	if strings.Contains(plain, "sky-tabs-vertical") {
		t.Errorf("未开竖向时不该产出竖向规则:\n%s", plain)
	}
	right := tabsCSSFor(&Props{Tabs: []Tab{{Label: "A"}}, NavAlign: "right", Vertical: true})
	if !strings.Contains(right, "justify-content: flex-end;") {
		t.Errorf("右对齐应翻成 flex-end:\n%s", right)
	}
	for _, want := range []string{".sky-c-t.sky-tabs-vertical {", ".sky-c-t.sky-tabs-vertical .sky-tabs-nav {", ".sky-c-t.sky-tabs-vertical .sky-tab-panel {"} {
		if !strings.Contains(right, want) {
			t.Errorf("竖向规则缺少 %q\n%s", want, right)
		}
	}
}

// TestTabsCSSKeyframes 渐显关键帧与无残留占位。
func TestTabsCSSKeyframes(t *testing.T) {
	out := tabsCSSFor(&Props{Tabs: []Tab{{Label: "A"}}})
	if !strings.Contains(out, "@keyframes sky-tabs-fade {") {
		t.Errorf("渐显关键帧缺失:\n%s", out)
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "&") {
		t.Errorf("产物里残留占位或未替换的作用域前缀:\n%s", out)
	}
}
