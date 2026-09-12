package infobox

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func infoCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// TestInfoboxCSSAlign 对齐三档映射到 flex 与文本对齐两位。
func TestInfoboxCSSAlign(t *testing.T) {
	center := infoCSSFor(&Props{})
	for _, want := range []string{"align-items: center;", "text-align: center;"} {
		if !strings.Contains(center, want) {
			t.Errorf("默认居中缺失 %q:\n%s", want, center)
		}
	}
	left := infoCSSFor(&Props{Align: "left"})
	if !strings.Contains(left, "align-items: flex-start;") || !strings.Contains(left, "text-align: left;") {
		t.Errorf("左对齐映射有误:\n%s", left)
	}
	right := infoCSSFor(&Props{Align: "right"})
	if !strings.Contains(right, "align-items: flex-end;") || !strings.Contains(right, "text-align: right;") {
		t.Errorf("右对齐映射有误:\n%s", right)
	}
}

// TestInfoboxCSSOptionalColors 可选色各是独立规则，缺失即不产出。
func TestInfoboxCSSOptionalColors(t *testing.T) {
	plain := infoCSSFor(&Props{})
	if strings.Count(plain, ".sky-c-t .sky-infobox-icon {") != 1 {
		t.Errorf("未配色时图标只应有基础那一条规则:\n%s", plain)
	}
	if strings.Contains(plain, "opacity: 1;") {
		t.Errorf("未设正文色时不该把透明度改回 1:\n%s", plain)
	}

	full := infoCSSFor(&Props{IconColor: "#f00", IconBgColor: "#eee", IconBorderColor: "#ccc", TitleColor: "#111", TextColor: "#555"})
	for _, want := range []string{
		"color: #f00;",
		"background: #eee;",
		"border: 1px solid #ccc;",
		"color: #111;",
		"color: #555;",
		"opacity: 1;",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("产物缺少 %q\n%s", want, full)
		}
	}
}

// TestInfoboxCSSSwitches 副标题、圆角、按钮都是成组的开关。
func TestInfoboxCSSSwitches(t *testing.T) {
	plain := infoCSSFor(&Props{})
	for _, notWant := range []string{"sky-infobox-subtitle", "overflow: hidden", "sky-infobox-btn"} {
		if strings.Contains(plain, notWant) {
			t.Errorf("未开开关时不该出现 %q:\n%s", notWant, plain)
		}
	}
	full := infoCSSFor(&Props{Subtitle: "标签", Radius: "12px", BtnText: "了解更多"})
	for _, want := range []string{
		".sky-c-t .sky-infobox-subtitle {",
		"border-radius: 12px;",
		"overflow: hidden;",
		".sky-c-t .sky-infobox-btn {",
		".sky-c-t .sky-infobox-btn:hover {\n  opacity: .85;",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("产物缺少 %q\n%s", want, full)
		}
	}
}
