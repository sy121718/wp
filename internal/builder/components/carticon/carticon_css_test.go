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
