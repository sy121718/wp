package carticon

// carticon_css_test.go — 购物车图标的样式编译测试。
//
// 关注点同样是**多端硬规则**与形态差异：触发区够不够点、浮层会不会横向溢出、
// 抽屉在移动端会不会被地址栏吃掉底部、悬停规则有没有只发给支持悬停的设备。

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

// TestCompileCSSDefaults 缺省形态（下拉）：基础规则齐、抽屉/弹窗规则不出现。
func TestCompileCSSDefaults(t *testing.T) {
	css := cartIconCSSFor(t, baseProps())
	for _, want := range []string{
		"min-height: 44px",         // 触屏可点区域（图标本身只有 24px）
		"min(100vw - 32px, 340px)", // 浮层宽度按视口封顶
		"width: 24px",              // 图标尺寸
		"list-style: none",         // 去掉 summary 的三角标记
		":hover",                   // 交互反馈带伪类
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

// TestCompileCSSDrawer 侧边抽屉：贴边固定 + dvh（移动端地址栏）。
func TestCompileCSSDrawer(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeDrawer})
	for _, want := range []string{
		"position: fixed",
		"100dvh", // 用 dvh 而不是 vh：地址栏收放时 vh 会让底部跑到屏幕外
		"width: min(100%, 380px)",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("抽屉样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}

// TestCompileCSSDrawerLeft 抽屉贴左侧：覆盖默认的右侧贴边。
func TestCompileCSSDrawerLeft(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeDrawer, DrawerSide: "left"})
	if !strings.Contains(css, "left: 0") {
		t.Fatalf("左侧抽屉应贴左；实际样式：\n%s", css)
	}
}

// TestCompileCSSModal 居中弹窗：定位 + 遮罩。
func TestCompileCSSModal(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeModal})
	// dvh 是移动端的关键：vh 在地址栏收放时会让面板底部跑到屏幕外。
	for _, want := range []string{"translate(-50%, -50%)", "rgba(17, 24, 39, 0.45)", "dvh"} {
		if !strings.Contains(css, want) {
			t.Fatalf("弹窗样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
}

// TestCompileCSSHoverMediaQuery 悬停形态：hover 规则必须包在 @media (hover: hover) 里。
//
// 触屏上那类规则根本不该输出 —— 否则手机上会「点一下卡住悬停态」（粘滞）。
func TestCompileCSSHoverMediaQuery(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeHover})
	if !strings.Contains(css, "@media (hover: hover)") {
		t.Fatalf("悬停形态的 hover 规则必须包在 @media (hover: hover) 里；实际样式：\n%s", css)
	}
	if !strings.Contains(css, ":focus-within") {
		t.Fatalf("悬停形态要给键盘用户留一条路（focus-within）；实际样式：\n%s", css)
	}
}

// TestCompileCSSColorOverride 自定义主色覆盖缺省。
func TestCompileCSSColorOverride(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Color: "#c00"})
	if !strings.Contains(css, "#c00") {
		t.Fatalf("自定义颜色未生效：\n%s", css)
	}
}

// TestCompileCSSScopedPerInstance 作用域按 node id 派生（同页两个图标互不污染）。
func TestCompileCSSScopedPerInstance(t *testing.T) {
	var b1, b2 core.CSSBuckets
	CompileCSS("iconA", baseProps(), &b1)
	CompileCSS("iconB", baseProps(), &b2)
	if b1.String() == b2.String() {
		t.Fatal("不同 node id 的样式应各自作用域化")
	}
}

// cssRuleText 取产物里以 header 开头的那一段（含配对的大括号）。
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

// cssMediaBlocks 收集产物里**全部**以 header 开头的块并拼接。
//
// 同一条媒体查询会为每个选择器各出一个块（CSSBuckets 按规则进桶），
// 只取第一块会漏掉后面几条 —— 触屏那条路正好由多条规则组成。
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

// stripMediaBlocks 去掉产物里的全部 @media 块，只留裸规则。
//
// 用来断言某条规则**不在**媒体查询里 —— 触屏粘滞这类问题只体现在「规则落到哪一层」，
// 单看子串是否存在是看不出来的。
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

// TestCompileCSSHoverTouchToggle 触屏等价形态：hover 形态在没有悬停的设备上
// 必须给出「点击展开」那条路（@media (hover: none)），而不是让浮层功能整体消失。
//
// 与导航子菜单同一个坑：靠 :hover 展开的内容在触屏上不是样式差异，是功能不存在。
func TestCompileCSSHoverTouchToggle(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeHover})
	touch := cssMediaBlocks(t, css, "@media (hover: none)")

	for _, want := range []string{
		":checked ~ .sky-cart-icon-panel", // 点击展开的驱动点
		"clip-path: inset(50%)",           // sr-only：视觉隐藏但**仍可聚焦**（键盘要能开合）
		"inset: 0",                        // 覆盖层铺满触发区，手指点哪里都算
	} {
		if !strings.Contains(touch, want) {
			t.Fatalf("触屏等价形态缺少 %q；实际样式：\n%s", want, css)
		}
	}
	// display:none 的控件点不动 label、也不进键盘序列 —— 触屏上它是唯一的展开入口。
	if strings.Contains(touch, "display: none") {
		t.Fatalf("触屏开合控件被彻底隐藏了（应是 sr-only，不是 display:none）；实际样式：\n%s", css)
	}
	// 控件默认隐藏：桌面点击仍是原来的「点图标去购物车页」，不被这层覆盖接管。
	if !strings.Contains(css, ".sky-cart-icon-toggle, .sky-c-t .sky-cart-icon-cover {") {
		t.Fatalf("开合控件与覆盖层应默认隐藏；实际样式：\n%s", css)
	}
}

// TestCompileCSSHoverFocusWithinIsHoverOnly 键盘那条展开路径必须只发给支持悬停的设备。
//
// 触屏上点 label 会让那个 checkbox 拿到焦点：:focus-within 若裸输出，
// 「展开后再点收起」收不回去 —— 焦点还在，规则一直按着它展开。
// 触屏的开合由 @hovernone 的 :checked 负责，两者互斥。
func TestCompileCSSHoverFocusWithinIsHoverOnly(t *testing.T) {
	css := cartIconCSSFor(t, &Props{Mode: ModeHover})
	if hover := cssRuleText(t, css, "@media (hover: hover)"); !strings.Contains(hover, ":focus-within") {
		t.Fatalf("键盘用户那条路（:focus-within）应包在 @media (hover: hover) 里；实际样式：\n%s", css)
	}
	if plain := stripMediaBlocks(css); strings.Contains(plain, ":focus-within") {
		t.Fatalf(":focus-within 出现在裸规则里（触屏上展开后收不回去）；实际样式：\n%s", css)
	}
}

// TestCompileCSSOtherModesKeepNoTouchToggle 其余三种形态走原生 details，
// 不该被这层触屏覆盖沾上（两套开合机制并存只会互相打架）。
func TestCompileCSSOtherModesKeepNoTouchToggle(t *testing.T) {
	for _, mode := range []string{ModeDropdown, ModeDrawer, ModeModal} {
		css := cartIconCSSFor(t, &Props{Mode: mode})
		if strings.Contains(css, "sky-cart-icon-toggle") || strings.Contains(css, "sky-cart-icon-cover") {
			t.Fatalf("形态 %s 不该出现触屏开合控件；实际样式：\n%s", mode, css)
		}
	}
}
