package divider

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestDividerCSSNoInset 无嵌入：纯线 + margin 归零，不产出任何嵌入相关规则。
func TestDividerCSSNoInset(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Style: "solid", Weight: "2px", Color: "#ccc"}, &b)
	out := b.String()
	for _, want := range []string{".sky-c-t {", "border-top: 2px solid #ccc", "margin: 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	for _, nw := range []string{".dt-line", ".dt-inset", "display: flex"} {
		if strings.Contains(out, nw) {
			t.Errorf("无嵌入时不该产出 %q\n%s", nw, out)
		}
	}
}

// TestDividerCSSWithInset 有嵌入：flex 容器 + 两段线 + 嵌入元素，位置决定线比例。
func TestDividerCSSWithInset(t *testing.T) {
	cases := []struct {
		name                string
		position            string
		leftFlex, rightFlex string
	}{
		{"靠左（左短右长）", PosLeft, "0.5", "1.5"},
		{"靠右（左长右短）", PosRight, "1.5", "0.5"},
		{"居中（等分）", PosCenter, "1", "1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b core.CSSBuckets
			compileCSS("t", &Props{Inset: Inset{Kind: InsetText, Text: "或者", Position: c.position, Spacing: "8px", FontSize: "14px"}}, &b)
			out := b.String()
			for _, want := range []string{
				"display: flex",
				"width: 100%",
				".sky-c-t .dt-line {",
				".sky-c-t .dt-line:last-child {",
				"flex: " + c.leftFlex + ";",
				"flex: " + c.rightFlex + ";",
				".sky-c-t .dt-inset {",
				"padding: 0 8px",
				"white-space: nowrap",
				"font-size: 14px",
				".sky-c-t .dt-inset svg {",
				"width: 1em",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("产物缺少 %q\n%s", want, out)
				}
			}
			// 有嵌入时不产出纯线形态。
			if strings.Contains(out, "margin: 0") {
				t.Errorf("有嵌入时不该产出纯线的 margin: 0\n%s", out)
			}
		})
	}
}

// TestDividerCSSWidthAndAlign 宽度三端与对齐：宽度为空则该端整组（width + margin）不产出。
func TestDividerCSSWidthAndAlign(t *testing.T) {
	var b core.CSSBuckets
	compileCSS("t", &Props{Align: "left", Width: Width{Desktop: "60%", Mobile: "100%"}}, &b)
	out := b.String()
	for _, want := range []string{"width: 60%", "margin-left: 0", "margin-right: auto", "width: 100%"} {
		if !strings.Contains(out, want) {
			t.Errorf("产物缺少 %q\n%s", want, out)
		}
	}
	// 平板端未设宽度：产物里只应有两处 width（桌面 + 手机）。
	if n := strings.Count(out, "width: 6"); n != 1 {
		t.Errorf("桌面宽度应只出现一次，实际 %d 次:\n%s", n, out)
	}

	// 宽度全空 → 不产出宽度规则（但纯线规则仍在）。
	var b2 core.CSSBuckets
	compileCSS("t", &Props{Align: "right"}, &b2)
	out2 := b2.String()
	if strings.Contains(out2, "margin-left") || strings.Contains(out2, "width:") {
		t.Errorf("宽度为空不该产出宽度与对齐声明:\n%s", out2)
	}
}
