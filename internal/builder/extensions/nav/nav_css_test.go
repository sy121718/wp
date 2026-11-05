package nav

import (
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

func navCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

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

func TestNavCSSSubmenuBg(t *testing.T) {
	if !strings.Contains(navCSSFor(&Props{}), "background: var(--sky-c-surface, #fff);") {
		t.Errorf("子菜单底色应兜底为主题面")
	}
	if !strings.Contains(navCSSFor(&Props{SubmenuBg: "#eee"}), "background: #eee;") {
		t.Errorf("自定义子菜单底色缺失")
	}

	if !strings.Contains(navCSSFor(&Props{SubmenuWidth: "240px"}), "min-width: min(100%, 240px);") {
		t.Errorf("子菜单宽度未覆盖，或没有按容器收口（应写成 min(100%%, 240px)）")
	}
}

func TestNavCSSMobileCollapse(t *testing.T) {
	plain := navCSSFor(&Props{})
	if strings.Contains(plain, "sky-nav-toggle") {
		t.Errorf("未开折叠时不该产出开关规则:\n%s", plain)
	}
	full := navCSSFor(&Props{MobileCollapse: true})

	for _, want := range []string{
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

func TestNavCSSSubmenuFocusWithin(t *testing.T) {
	out := navCSSFor(&Props{})
	want := "@media (hover: hover) {\n  .sky-c-t .sky-nav-item:focus-within > .sky-nav-sub {"
	if !strings.Contains(out, want) {
		t.Errorf("键盘展开应包在 (hover: hover) 里（触屏要靠 checkbox 收起）:\n%s", out)
	}
}

func TestNavCSSSubmenuTouchToggle(t *testing.T) {
	out := navCSSFor(&Props{})

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

		"clip-path: inset(50%);",
		".sky-c-t .sky-nav-sub-cover {",
		"inset: 0;",

		".sky-c-t .sky-nav-sub-toggle:checked ~ .sky-nav-sub {",

		".sky-c-t .sky-nav-sub-toggle:checked ~ .sky-nav-sub-cover {",

		".sky-c-t .sky-nav-sub-chevron {",
		".sky-c-t .sky-nav-sub-toggle:focus-visible ~ .sky-nav-sub-chevron {",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("触屏块缺少 %q\n%s", want, joined)
		}
	}

	for _, b := range blocks {
		if strings.Contains(b, ":hover") {
			t.Errorf("(hover: none) 块里不得出现 :hover（触屏永不匹配）:\n%s", b)
		}
	}
}

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

func TestNavCSSSubmenuHoverUnchanged(t *testing.T) {
	out := navCSSFor(&Props{})
	if !strings.Contains(out, ".sky-c-t .sky-nav-item:hover > .sky-nav-sub {\n  opacity: 1;\n  visibility: visible;") {
		t.Errorf("鼠标悬停展开规则被改动了:\n%s", out)
	}
	if !strings.Contains(out, ".sky-c-t .sky-nav-item {\n  position: relative;") {
		t.Errorf("覆盖层依赖的定位上下文（position: relative）不该被改动:\n%s", out)
	}
}
