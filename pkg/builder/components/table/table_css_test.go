package table

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func tableCSSFor(p *Props) (core.CSSBuckets, string) {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b, b.String()
}

func TestTableCSSSwitches(t *testing.T) {
	_, plain := tableCSSFor(&Props{})
	for _, notWant := range []string{"nth-child(even)", "transition: background", "border: 1px solid"} {
		if strings.Contains(plain, notWant) {
			t.Errorf("未开开关时不该产出 %q:\n%s", notWant, plain)
		}
	}

	_, striped := tableCSSFor(&Props{Striped: true})
	if !strings.Contains(striped, "& tbody tr:nth-child(even)") && !strings.Contains(striped, ".sky-c-t tbody tr:nth-child(even)") {
		t.Errorf("斑马纹规则缺失:\n%s", striped)
	}

	_, bordered := tableCSSFor(&Props{Bordered: true})
	if strings.Count(bordered, ".sky-c-t th, .sky-c-t td {") != 2 {
		t.Errorf("开启边框后应有两条第 3 列规则（基础 + 边框）:\n%s", bordered)
	}
}

func TestTableCSSRowHover(t *testing.T) {
	_, out := tableCSSFor(&Props{RowHover: true})
	if !strings.Contains(out, "transition: background 0.15s ease;") {
		t.Errorf("行的过渡声明缺失（丢了就成瞬变）:\n%s", out)
	}
	if !strings.Contains(out, "@media (hover: hover) {") || !strings.Contains(out, "background: rgba(0,0,0,0.05);") {
		t.Errorf("悬停高亮未进 (hover: hover) 桶:\n%s", out)
	}
	if strings.Contains(out, "(hover: none)") {
		t.Errorf("表格行悬停不需要触屏等价形态:\n%s", out)
	}
}
