package nav

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func navCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

// TestNavCSSVertical 竖向开关把两处声明合并进同一条列表规则。
func TestNavCSSVertical(t *testing.T) {
	plain := navCSSFor(&Props{})
	if strings.Contains(plain, "flex-direction: column;") {
		t.Errorf("横向布局不该产出竖向声明:\n%s", plain)
	}
	vert := navCSSFor(&Props{Orientation: "vertical"})
	want := ".sky-c-t .sky-nav-list {\n  display: flex;\n  align-items: center;\n  list-style: none;\n  margin: 0;\n  padding: 0;\n  flex-direction: column;\n  align-items: flex-start;"
	if !strings.Contains(vert, want) {
		t.Errorf("竖向声明应与基础声明合并进同一条规则（拆开就不是等价迁移了）:\n%s", vert)
	}
}

// TestNavCSSAlign 对齐档位映射；未设时不产出该声明。
func TestNavCSSAlign(t *testing.T) {
	if strings.Contains(navCSSFor(&Props{}), "justify-content:") {
		t.Errorf("未设对齐时不该产出 justify-content")
	}
	if !strings.Contains(navCSSFor(&Props{Align: "center"}), "justify-content: center;") {
		t.Errorf("居中对齐映射有误")
	}
	if !strings.Contains(navCSSFor(&Props{Align: "right"}), "justify-content: flex-end;") {
		t.Errorf("右对齐映射有误")
	}
}

// TestNavCSSColors 悬停色命中两处（顶级项与子项），激活色命中两处（顶级与子项当前）。
func TestNavCSSColors(t *testing.T) {
	plain := navCSSFor(&Props{})
	if strings.Contains(plain, ":hover {") && strings.Count(plain, "color: ") > 2 {
		t.Errorf("未设悬停色时不该多出着色规则:\n%s", plain)
	}
	full := navCSSFor(&Props{HoverColor: "#f00", ActiveColor: "#0f0"})
	if strings.Count(full, "color: #f00;") != 2 {
		t.Errorf("悬停色应命中顶级项与子项两处:\n%s", full)
	}
	if strings.Count(full, "color: #0f0;") != 2 {
		t.Errorf("激活色应命中顶级项与子项当前两处:\n%s", full)
	}
}

// TestNavCSSSubmenuBg 子菜单底色未配时跟主题面。
func TestNavCSSSubmenuBg(t *testing.T) {
	if !strings.Contains(navCSSFor(&Props{}), "background: var(--sky-c-surface, #fff);") {
		t.Errorf("子菜单底色应兜底为主题面")
	}
	if !strings.Contains(navCSSFor(&Props{SubmenuBg: "#eee"}), "background: #eee;") {
		t.Errorf("自定义子菜单底色缺失")
	}
	if !strings.Contains(navCSSFor(&Props{SubmenuWidth: "240px"}), "min-width: 240px;") {
		t.Errorf("子菜单宽度未覆盖")
	}
}

// TestNavCSSMobileCollapse 折叠开关：sr-only 的 checkbox 必须可聚焦。
//
// hidden 属性的元素不进键盘序列，键盘用户无法展开移动端菜单 ——
// 所以这里刻意用 clip-path 的 sr-only 写法，而不是 display:none / hidden。
func TestNavCSSMobileCollapse(t *testing.T) {
	plain := navCSSFor(&Props{})
	if strings.Contains(plain, "sky-nav-toggle") {
		t.Errorf("未开折叠时不该产出开关规则:\n%s", plain)
	}
	full := navCSSFor(&Props{MobileCollapse: true})
	for _, want := range []string{
		".sky-c-t .sky-nav-toggle {",
		"clip-path: inset(50%);",
		".sky-c-t .sky-nav-toggle:focus-visible + .sky-nav-burger {",
		"@media (max-width: 767px) {",
		".sky-c-t .sky-nav-toggle:checked ~ .sky-nav-list {\n  display: flex;",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("产物缺少 %q\n%s", want, full)
		}
	}
	if strings.Contains(full, "display: none;\n  \n") {
		t.Errorf("折叠开关不该用 display:none 隐藏（会退出键盘序列）")
	}
}
