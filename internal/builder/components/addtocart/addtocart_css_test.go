package addtocart

// addtocart_css_test.go — 加购组件的样式编译测试。
//
// 关注点是**多端硬规则**（docs/02-C0 §6.9）：宽度不写死、触屏可点区域、
// hover 形态只输出给支持 hover 的环境。这几条写错了在桌面浏览器上看不出来 ——
// 只有手机上或触屏模拟里才暴露，所以必须由断言钉住。

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func addToCartCSSFor(t *testing.T, p *Props) string {
	t.Helper()
	var b core.CSSBuckets
	CompileCSS("t", p, &b)
	return b.String()
}

// TestCompileCSSDefaults 缺省模式：基础盒模型 + 按钮 + 数量输入。
func TestCompileCSSDefaults(t *testing.T) {
	css := addToCartCSSFor(t, baseProps())
	for _, want := range []string{
		"width: min(100%, 520px)",                   // 宽度不写死
		"min-height: 44px",                          // 触屏可点区域
		"background: var(--sky-c-primary, #171717)", // 缺省主色走主题 Token
		"@media (hover: hover)",                     // hover 形态只在支持 hover 的环境输出
		"filter: brightness(0.94)",                  // 按压反馈（不带媒体查询，触屏也要有）
		":hover",                                    // 交互反馈必须带伪类：AddHover/AddActive 不替你加，
		":active",                                   // 漏了不是「没反馈」而是「反馈恒定生效」
		".sky-cart-add-row", ".sky-cart-add-qty", ".sky-cart-add-btn",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("缺省样式缺少 %q；实际样式：\n%s", want, css)
		}
	}
	// 逐变体独有规则不该出现在缺省模式里。
	for _, unwanted := range []string{"padding-top: var(--sky-space-5, 10px)", "width: min(100%, 240px)"} {
		if strings.Contains(css, unwanted) {
			t.Fatalf("缺省模式不该输出逐变体独有规则 %q；实际样式：\n%s", unwanted, css)
		}
	}
}

// TestCompileCSSColorOverride 自定义颜色覆盖缺省主色。
func TestCompileCSSColorOverride(t *testing.T) {
	p := baseProps()
	p.Color = "#f00"
	css := addToCartCSSFor(t, p)
	if !strings.Contains(css, "background: #f00") {
		t.Fatalf("自定义颜色未生效：\n%s", css)
	}
	if strings.Contains(css, "#171717") {
		t.Fatalf("自定义颜色时不该再出现缺省主色：\n%s", css)
	}
}

// TestCompileCSSScopedPerInstance 作用域按 node id 派生（同页多实例互不污染）。
func TestCompileCSSScopedPerInstance(t *testing.T) {
	var b1, b2 core.CSSBuckets
	CompileCSS("nodeA", baseProps(), &b1)
	CompileCSS("nodeB", baseProps(), &b2)
	if b1.String() == b2.String() {
		t.Fatal("不同 node id 的样式应各自作用域化（同页两个加购组件不能互相覆盖）")
	}
	if !strings.Contains(b1.String(), core.NodeClass("nodeA")) {
		t.Fatalf("样式未绑定到本实例的类名：\n%s", b1.String())
	}
}
