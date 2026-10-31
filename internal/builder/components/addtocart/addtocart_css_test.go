package addtocart

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

func TestCompileCSSDefaults(t *testing.T) {
	css := addToCartCSSFor(t, baseProps())
	for _, want := range []string{
		"width: min(100%, 520px)",
		"min-height: 44px",
		"background: var(--sky-c-primary, #171717)",
		"@media (hover: hover)",
		"filter: brightness(0.94)",
		":hover",
		":active",
		".sky-cart-add-row", ".sky-cart-add-qty", ".sky-cart-add-btn",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("缺省样式缺少 %q；实际样式：\n%s", want, css)
		}
	}

	for _, unwanted := range []string{"padding-top: var(--sky-space-5, 10px)", "width: min(100%, 240px)"} {
		if strings.Contains(css, unwanted) {
			t.Fatalf("缺省模式不该输出逐变体独有规则 %q；实际样式：\n%s", unwanted, css)
		}
	}
}

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
