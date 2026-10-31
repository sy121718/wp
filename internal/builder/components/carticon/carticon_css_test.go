package carticon

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func cartIconCSSFor(t *testing.T, p *Props) string {
	t.Helper()
	var b core.CSSBuckets
	CompileCSS("t", p, &b)
	return b.String()
}

func TestCompileCSSDefaults(t *testing.T) {
	css := cartIconCSSFor(t, baseProps())
	for _, want := range []string{
		"min-height: 44px",
		"min(100vw - 32px, 340px)",
		"width: 24px",
		"list-style: none",
		":hover",
		":active",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("缺省样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
	for _, unwanted := range []string{"100dvh", "translate(-50%, -50%)"} {
		if strings.Contains(css, unwanted) {
			t.Fatalf("下拉形态不该包含其它形态的规则 %q；实际样式：\n%s", unwanted, css)
		}
	}
}

func TestCompileCSSDrawer(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeDrawer})
	for _, want := range []string{
		"position: fixed",
		"100dvh",
		"width: min(100%, 380px)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("抽屉样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}

func TestCompileCSSDrawerLeft(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeDrawer, DrawerSide: "left"})
	if !strings.Contains(css, "left: 0") {
		t.Fatalf("左侧抽屉应贴左；实际样式：\n%s", css)
	}
}

func TestCompileCSSModal(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeModal})

	for _, want := range []string{"translate(-50%, -50%)", "rgba(17, 24, 39, 0.45)", "dvh"} {
		if !strings.Contains(css, want) {
			t.Fatalf("弹窗样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}

func TestCompileCSSHoverMediaQuery(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeHover})
	if !strings.Contains(css, "@media (hover: hover)") {
		t.Fatalf("悬停形态的 hover 规则必须包在 @media (hover: hover) 里；实际样式：\n%s", css)
	}
	if !strings.Contains(css, ":focus-within") {
		t.Fatalf("悬停形态要给键盘用户留一条路（focus-within）；实际样式：\n%s", css)
	}
}

func TestCompileCSSColorOverride(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Color: "#c00"})
	if !strings.Contains(css, "#c00") {
		t.Fatalf("自定义颜色未生效：\n%s", css)
	}
}

func TestCompileCSSScopedPerInstance(t *testing.T) {
	var b1, b2 core.CSSBuckets
	CompileCSS("iconA", baseProps(), &b1)
	CompileCSS("iconB", baseProps(), &b2)
	if b1.String() == b2.String() {
		t.Fatal("不同 node id 的样式应各自作用域化")
	}
}

func cssRuleText(t *testing.T, css, header string) string {
	t.Helper()
	i := strings.Index(css, header)
	if i < 0 {
		t.Fatalf("产物里找不到 %q:\n%s", header, css)
	}
	depth := 0
	for j := i; j < len(css); j++ {
		switch css[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[i : j+1]
			}
		}
	}
	t.Fatalf("%q 的花括号没有闭合:\n%s", header, css)
	return ""
}

func cssMediaBlocks(t *testing.T, css, header string) string {
	t.Helper()
	var sb strings.Builder
	offset, found := 0, 0
	for {
		i := strings.Index(css[offset:], header)
		if i < 0 {
			break
		}
		start := offset + i
		depth, end := 0, start
		for j := start; j < len(css); j++ {
			switch css[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 && css[j] == '}' {
				end = j
				break
			}
		}
		sb.WriteString(css[start : end+1])
		sb.WriteString("\n")
		found++
		offset = end + 1
	}
	if found == 0 {
		t.Fatalf("产物里找不到 %q:\n%s", header, css)
	}
	return sb.String()
}

func stripMediaBlocks(css string) string {
	var sb strings.Builder
	for i := 0; i < len(css); {
		if strings.HasPrefix(css[i:], "@media") {
			depth := 0
			j := i
			for ; j < len(css); j++ {
				switch css[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
				if depth == 0 && css[j] == '}' {
					break
				}
			}
			i = j + 1
			continue
		}
		sb.WriteByte(css[i])
		i++
	}
	return sb.String()
}

func TestCompileCSSHoverTouchToggle(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeHover})
	touch := cssMediaBlocks(t, css, "@media (hover: none)")

	for _, want := range []string{
		":checked ~ .sky-cart-icon-panel",
		"clip-path: inset(50%)",
		"inset: 0",
	} {
		if !strings.Contains(touch, want) {
			t.Fatalf("触屏等价形态缺少 %q；实际样式：\n%s", want, css)
		}
	}

	if strings.Contains(touch, "display: none") {
		t.Fatalf("触屏开合控件被彻底隐藏了（应是 sr-only，不是 display:none）；实际样式：\n%s", css)
	}

	if !strings.Contains(css, ".sky-cart-icon-toggle, .sky-c-t .sky-cart-icon-cover {") {
		t.Fatalf("开合控件与覆盖层应默认隐藏；实际样式：\n%s", css)
	}
}

func TestCompileCSSHoverFocusWithinIsHoverOnly(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeHover})
	if hover := cssRuleText(t, css, "@media (hover: hover)"); !strings.Contains(hover, ":focus-within") {
		t.Fatalf("键盘用户那条路（:focus-within）应包在 @media (hover: hover) 里；实际样式：\n%s", css)
	}
	if plain := stripMediaBlocks(css); strings.Contains(plain, ":focus-within") {
		t.Fatalf(":focus-within 出现在裸规则里（触屏上展开后收不回去）；实际样式：\n%s", css)
	}
}

func TestCompileCSSOtherModesKeepNoTouchToggle(t *testing.T) {
	for _, mode := range []string{ModeDropdown, ModeDrawer, ModeModal} {
		css := cartIconCSSFor(t, &Props{Mode: mode})
		if strings.Contains(css, "sky-cart-icon-toggle") || strings.Contains(css, "sky-cart-icon-cover") {
			t.Fatalf("形态 %s 不该出现触屏开合控件；实际样式：\n%s", mode, css)
		}
	}
}
