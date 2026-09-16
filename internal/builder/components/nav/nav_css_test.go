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
	// 宽度是「设计宽度」不是定值：必须写成 min(100%, Npx) 才能在窄视口收口
	// （多端硬规则；审计 UI-015 的产物级守卫就是在这条规则上抓到子菜单的）。
	if !strings.Contains(navCSSFor(&Props{SubmenuWidth: "240px"}), "min-width: min(100%, 240px);") {
		t.Errorf("子菜单宽度未覆盖，或没有按容器收口（应写成 min(100%%, 240px)）")
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

// hovernoneBlocks 取出产物里所有 (hover: none) 媒体块（含各自完整的大括号范围）。
func hovernoneBlocks(css string) []string {
	const header = "@media (hover: none) {"
	var out []string
	for {
		i := strings.Index(css, header)
		if i < 0 {
			return out
		}
		rest := css[i:]
		depth, end := 0, -1
		for j, ch := range rest {
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j + 1
				}
			}
			if end > 0 {
				break
			}
		}
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		css = rest[end:]
	}
}

// TestNavCSSSubmenuFocusWithin 键盘路径：焦点落进该项就展开。
//
// 必须裹在 (hover: hover) 里。裸写会让触屏「收不起来」：点 label 后 checkbox 拿到焦点，
// :focus-within 一直按着展开态。触屏那条路走 @hovernone，两套互斥。
func TestNavCSSSubmenuFocusWithin(t *testing.T) {
	out := navCSSFor(&Props{})
	want := "@media (hover: hover) {\n  .sky-c-t .sky-nav-item:focus-within > .sky-nav-sub {"
	if !strings.Contains(out, want) {
		t.Errorf("键盘展开应包在 (hover: hover) 里（触屏要靠 checkbox 收起）:\n%s", out)
	}
}

// TestNavCSSSubmenuTouchToggle 触屏路径：没有悬停，子菜单改由「点击展开」驱动。
//
// 触屏上 :hover 永不触发，这条链路是子菜单唯一的入口，所以三点都要钉住：
// 默认不渲染（桌面不多出一个键盘停靠点）、触屏下 sr-only 但可聚焦、选中即展开。
func TestNavCSSSubmenuTouchToggle(t *testing.T) {
	out := navCSSFor(&Props{})

	// 默认（非触屏）不渲染：不占位，也不进键盘序列。
	for _, want := range []string{
		".sky-c-t .sky-nav-sub-toggle {",
		".sky-c-t .sky-nav-sub-cover {",
		".sky-c-t .sky-nav-sub-chevron {",
	} {
		if !strings.Contains(out, want+"\n  display: none;") {
			t.Errorf("非触屏下 %q 应整条不渲染:\n%s", want, out)
		}
	}

	blocks := hovernoneBlocks(out)
	if len(blocks) == 0 {
		t.Fatalf("触屏形态未产出 (hover: none) 块:\n%s", out)
	}
	joined := strings.Join(blocks, "\n")
	for _, want := range []string{
		".sky-c-t .sky-nav-sub-toggle {",
		// sr-only 而不是 display:none —— 后者不响应 label 点击，触屏就没入口了。
		"clip-path: inset(50%);",
		".sky-c-t .sky-nav-sub-cover {",
		"inset: 0;",
		// 展开：选中即显形。
		".sky-c-t .sky-nav-sub-toggle:checked ~ .sky-nav-sub {",
		// 展开后必须撤掉覆盖层，否则父链接永远点不到。
		".sky-c-t .sky-nav-sub-toggle:checked ~ .sky-nav-sub-cover {",
		// 触控目标与键盘焦点环。
		".sky-c-t .sky-nav-sub-chevron {",
		".sky-c-t .sky-nav-sub-toggle:focus-visible ~ .sky-nav-sub-chevron {",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("触屏块缺少 %q\n%s", want, joined)
		}
	}

	// 触屏块里的选择器绝不能带 :hover —— 带上就永不匹配，整段等于没写。
	for _, b := range blocks {
		if strings.Contains(b, ":hover") {
			t.Errorf("(hover: none) 块里不得出现 :hover（触屏永不匹配）:\n%s", b)
		}
	}
}

// TestNavCSSSubmenuTouchDisabledWhenCollapsed 折叠态的子菜单已是常显内联（没有悬停可用），
// 触屏开关在这里既多余又碍事：那层覆盖会吃掉父项链接的点击。
//
// 用多一级的选择器压 @hovernone —— 它在 hover 桶里排最后，同优先级后者胜。
func TestNavCSSSubmenuTouchDisabledWhenCollapsed(t *testing.T) {
	out := navCSSFor(&Props{MobileCollapse: true})
	for _, want := range []string{
		".sky-c-t .sky-nav-item .sky-nav-sub-toggle {",
		".sky-c-t .sky-nav-item .sky-nav-sub-cover {",
		".sky-c-t .sky-nav-item .sky-nav-sub-chevron {",
	} {
		if !strings.Contains(out, want+"\n    display: none;") && !strings.Contains(out, want+"\n  display: none;") {
			t.Errorf("折叠态应关掉触屏开关 %q:\n%s", want, out)
		}
	}
}

// TestNavCSSSubmenuHoverUnchanged 鼠标行为不变：悬停仍是裸 :hover 规则，
// 没被裹进任何媒体查询，也没被新增的两条链路改写。
func TestNavCSSSubmenuHoverUnchanged(t *testing.T) {
	out := navCSSFor(&Props{})
	if !strings.Contains(out, ".sky-c-t .sky-nav-item:hover > .sky-nav-sub {\n  opacity: 1;\n  visibility: visible;") {
		t.Errorf("鼠标悬停展开规则被改动了:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t .sky-nav-item {\n  position: relative;") {
		t.Errorf("覆盖层依赖的定位上下文（position: relative）不该被改动:\n%s", out)
	}
}
