package button

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

func buttonCSSFor(p *Props) string {
	var b core.CSSBuckets
	compileCSS("t", p, &b)
	return b.String()
}

func TestButtonCSSVariantBranches(t *testing.T) {
	solid := buttonCSSFor(&Props{})
	for _, want := range []string{
		"background: var(--sky-btn-bg, var(--sky-c-primary, #2563eb));",
		"color: var(--sky-btn-color, #fff);",
		"border: var(--sky-btn-border-width, 0) var(--sky-btn-border-style, solid) var(--sky-btn-border-color, transparent);",
	} {
		if !strings.Contains(solid, want) {
			t.Errorf("实心变体缺少 %q:\n%s", want, solid)
		}
	}
	if strings.Contains(solid, "border: 1px solid currentColor;") {
		t.Errorf("实心变体混进了轮廓分支的边框:\n%s", solid)
	}
	if strings.Contains(solid, "background: transparent;") {
		t.Errorf("实心变体混进了透明底分支:\n%s", solid)
	}

	outline := buttonCSSFor(&Props{Variant: "outline"})
	for _, want := range []string{
		"background: transparent;",
		"border: 1px solid currentColor;",
	} {
		if !strings.Contains(outline, want) {
			t.Errorf("轮廓变体缺少 %q:\n%s", want, outline)
		}
	}
	if strings.Contains(outline, "var(--sky-btn-bg") || strings.Contains(outline, "var(--sky-btn-color") {
		t.Errorf("轮廓变体混进了实心的主题回退链:\n%s", outline)
	}

	ghost := buttonCSSFor(&Props{Variant: "ghost", Text: "点我"})
	if !strings.Contains(ghost, "background: transparent;") {
		t.Errorf("幽灵变体缺少透明底:\n%s", ghost)
	}
	if strings.Contains(ghost, "1px solid") || strings.Contains(ghost, "var(--sky-btn-border-width") {
		t.Errorf("幽灵变体混进了边框:\n%s", ghost)
	}
	if strings.Contains(ghost, "padding: var(--sky-space-3, 6px) 0;") {
		t.Errorf("有文案的幽灵按钮不该收紧内边距:\n%s", ghost)
	}
	if !strings.Contains(buttonCSSFor(&Props{Variant: "ghost", Text: ""}), "padding: var(--sky-space-3, 6px) 0;") {
		t.Errorf("无文案的幽灵按钮应收紧内边距")
	}

	if !strings.Contains(buttonCSSFor(&Props{Variant: "ghostly"}), "var(--sky-btn-bg") {
		t.Errorf("未知变体应兜底为实心")
	}
}

func TestButtonCSSBlockScopes(t *testing.T) {
	rule := ".sky-c-t {\n  display: flex;\n  width: 100%;\n}"

	if none := buttonCSSFor(&Props{}); strings.Contains(none, "display: flex;") {
		t.Errorf("未开块级时不该产出 display: flex（基础规则是 inline-flex）:\n%s", none)
	}

	desk := buttonCSSFor(&Props{Block: Block{Desktop: true}})
	if !strings.Contains(desk, rule) {
		t.Errorf("桌面块级规则缺失:\n%s", desk)
	}
	if strings.Contains(desk, "@media (max-width: 1024px)") || strings.Contains(desk, "@media (max-width: 767px)") {
		t.Errorf("桌面块级不该进断点媒体查询:\n%s", desk)
	}

	tab := buttonCSSFor(&Props{Block: Block{Tablet: true}})
	if !strings.Contains(tab, "@media (max-width: 1024px) {\n"+rule+"\n}") {
		t.Errorf("平板块级规则缺失或未进 1024 断点:\n%s", tab)
	}
	if strings.Contains(tab, "@media (max-width: 767px)") {
		t.Errorf("只开平板时不该产出手机断点:\n%s", tab)
	}
	if strings.Count(tab, rule) != 1 {
		t.Errorf("平板块级规则条数应为 1（不得重复拼接）:\n%s", tab)
	}

	mob := buttonCSSFor(&Props{Block: Block{Mobile: true}})
	if !strings.Contains(mob, "@media (max-width: 767px) {\n"+rule+"\n}") {
		t.Errorf("手机块级规则缺失或未进 767 断点:\n%s", mob)
	}

	all := buttonCSSFor(&Props{Block: Block{Desktop: true, Tablet: true, Mobile: true}})
	if n := strings.Count(all, rule); n != 3 {
		t.Errorf("三端全开应有 3 条块级规则，实际 %d:\n%s", n, all)
	}

	for _, bad := range []string{".sky-c-t.sky-c-t", ".sky-c-t .sky-c-t"} {
		if strings.Contains(all, bad) {
			t.Errorf("选择器里多拼了作用域前缀 %q:\n%s", bad, all)
		}
	}
}

func TestButtonCSSHoverBranches(t *testing.T) {
	plain := buttonCSSFor(&Props{})
	if strings.Contains(plain, "transition:") {
		t.Errorf("没有任何悬停取值时不该产出过渡:\n%s", plain)
	}
	if strings.Contains(plain, ":hover") {
		t.Errorf("没有任何悬停取值时不该产出悬停规则:\n%s", plain)
	}

	exp := buttonCSSFor(&Props{HoverBg: "#f00"})

	if !strings.Contains(exp, "@media (hover: hover) {\n  .sky-c-t:hover, .sky-c-t:focus {") {
		t.Errorf("显式悬停色应进 (hover: hover) 桶并覆盖 :focus:\n%s", exp)
	}
	if !strings.Contains(exp, ".sky-c-t {\n  transition: all 0.2s ease;\n}") {
		t.Errorf("悬停声明应有配套过渡:\n%s", exp)
	}
	if !strings.Contains(exp, ".sky-c-t:hover, .sky-c-t:focus {\n  background: #f00;") {
		t.Errorf("悬停 / 聚焦态规则缺失:\n%s", exp)
	}

	full := buttonCSSFor(&Props{
		HoverBg: "#111", HoverColor: "#222", HoverBorderColor: "#333", HoverShadow: "lg", HoverLift: "-2px",
		TransitionDuration: "0.5s", TransitionEasing: "linear", TransitionDelay: "0.1s",
	})
	for _, want := range []string{
		"transition: all 0.5s linear 0.1s;",
		"background: #111;",
		"color: #222;",
		"border-color: #333;",
		"transform: translate(0, -2px);",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("悬停态缺少 %q:\n%s", want, full)
		}
	}

	o := buttonCSSFor(&Props{Variant: "outline", TextColor: "#123"})
	if !strings.Contains(o, ":hover, .sky-c-t:focus {\n  background: #123;") {
		t.Errorf("轮廓变体的悬停背景应回退文字色:\n%s", o)
	}
}

func TestButtonCSSIconShift(t *testing.T) {
	if strings.Contains(buttonCSSFor(&Props{}), "bt-icon-shift") {
		t.Errorf("没有图标时不该产出图标位移规则")
	}
	noShift := buttonCSSFor(&Props{Icon: &Icon{Source: "builtin", Name: "check", Spacing: "8px"}})
	if strings.Contains(noShift, "bt-icon-shift") {
		t.Errorf("未设位移时不该产出图标位移规则:\n%s", noShift)
	}
	if !strings.Contains(noShift, "gap: 8px;") {
		t.Errorf("图标间距缺失:\n%s", noShift)
	}

	shift := buttonCSSFor(&Props{Icon: &Icon{Source: "builtin", Name: "check", HoverShift: "4px"}})
	if !strings.Contains(shift, ".sky-c-t .bt-icon-shift {\n  transition: transform 0.2s ease;\n}") {
		t.Errorf("图标位移的过渡规则缺失:\n%s", shift)
	}
	if !strings.Contains(shift, ".sky-c-t:hover .bt-icon-shift {\n  transform: translateX(4px);") {
		t.Errorf("图标位移规则缺失:\n%s", shift)
	}

	stacked := buttonCSSFor(&Props{Icon: &Icon{Source: "builtin", Name: "check", Position: "top"}})
	if !strings.Contains(stacked, "flex-direction: column;") {
		t.Errorf("图标在上时按钮应改纵向排列:\n%s", stacked)
	}
	if strings.Contains(buttonCSSFor(&Props{Icon: &Icon{Source: "builtin", Name: "check", Position: "prefix"}}), "flex-direction: column;") {
		t.Errorf("图标在前时不该改纵向排列")
	}
}

func TestButtonCSSSizing(t *testing.T) {
	xs := buttonCSSFor(&Props{Size: "xs"})
	if !strings.Contains(xs, "padding: 4px 10px;") || !strings.Contains(xs, "font-size: 0.75rem;") {
		t.Errorf("xs 尺寸预设未命中:\n%s", xs)
	}
	unknown := buttonCSSFor(&Props{Size: "zzz"})
	if !strings.Contains(unknown, "padding: 10px 20px;") || !strings.Contains(unknown, "font-size: 1rem;") {
		t.Errorf("未知尺寸应回退默认档:\n%s", unknown)
	}
	if !strings.Contains(buttonCSSFor(&Props{}), "border-radius: var(--sky-btn-radius, 8px);") {
		t.Errorf("四角全空应回退主题圆角")
	}
	if !strings.Contains(buttonCSSFor(&Props{RadiusBL: "6px"}), "border-radius: 0 0 0 6px;") {
		t.Errorf("只设一角时其余应补 0（顺序 TL TR BR BL）")
	}

	out := buttonCSSFor(&Props{FontSize: "18px", Transform: "uppercase"})
	if !strings.Contains(out, "font-size: 18px;") || !strings.Contains(out, "text-transform: uppercase;") {
		t.Errorf("排版可选项缺失:\n%s", out)
	}
	if strings.Contains(buttonCSSFor(&Props{Transform: "none"}), "text-transform") {
		t.Errorf("transform=none 不该产出 text-transform")
	}
	for _, p := range []*Props{{}, {Variant: "ghost"}, {Variant: "outline"}, {Block: Block{Mobile: true}}} {
		got := buttonCSSFor(p)
		if strings.Contains(got, ": ;") || strings.Contains(got, ":;") {
			t.Errorf("产物里有无效声明:\n%s", got)
		}
		if strings.Contains(got, "{{") || strings.Contains(got, "&") {
			t.Errorf("产物里有未展开的占位或未替换的作用域前缀:\n%s", got)
		}
	}
}
