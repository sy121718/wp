package carticon

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.cartIcon"

const (
	ModeInline = "inline"

	ModeDropdown = "dropdown"

	ModeDrawer = "drawer"

	ModeModal = "modal"

	ModeHover = "hover"
)

const (
	cartViewPath    = "/api/site/cart"
	cartSummaryPath = "/api/site/cart"
)

const (
	defaultIcon  = "shopping-cart"
	defaultLabel = "购物车"

	defaultColor = "var(--sky-c-primary, #2563eb)"
)

type Props struct {
	Mode string `json:"mode,omitempty" ct:"select,inline=页面内完整购物车,dropdown=下拉浮层,drawer=侧边抽屉,modal=居中弹窗,hover=悬停浮层,default=dropdown,sec=content,label=展示形态"`

	Align string `json:"align,omitempty" ct:"select,right=右对齐,left=左对齐,default=right,sec=style,label=浮层对齐"`

	DrawerSide string `json:"drawerSide,omitempty" ct:"select,right=右侧,left=左侧,default=right,sec=style,label=抽屉方向"`

	Icon string `json:"icon,omitempty" ct:"string,maxlen=40,sec=style,label=图标名"`

	Label string `json:"label,omitempty" ct:"string,maxlen=20,sec=content,label=文字"`

	ShowLabel bool `json:"showLabel,omitempty" ct:"bool,sec=content,label=显示文字"`

	ShowCount bool `json:"showCount,omitempty" ct:"bool,sec=content,label=件数角标"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct{ core.Atom[Props] }

func (*Component) Actions() []core.ActionSpec {
	return []core.ActionSpec{{Key: "checkout", AllowedKinds: []core.ActionKind{core.ActionPage, core.ActionPresentation, core.ActionInstance}, TargetCapability: "commerce.checkout"}}
}
func (*Component) RequiredActions() []string { return nil }
func (*Component) Parts() []core.PartSpec {
	return []core.PartSpec{{Key: "cart", Capabilities: []string{"commerce.cart"}}}
}

var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "购物车图标",
		Hint:            "页头购物车入口（下拉 / 抽屉 / 弹窗 / 悬停）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"mode":       "dropdown",
			"align":      "right",
			"drawerSide": "right",
			"icon":       "shopping-cart",
			"label":      "购物车",
			"showLabel":  false,
			"showCount":  true,
		},
		TypeName: Type,

		Translatable: []string{"label"},
	},
}}

func effectiveMode(p *Props) string {
	switch p.Mode {
	case ModeInline, ModeDrawer, ModeModal, ModeHover:
		return p.Mode
	default:
		return ModeDropdown
	}
}

func effectiveLabel(p *Props) string {
	if l := strings.TrimSpace(p.Label); l != "" {
		return l
	}
	return defaultLabel
}

func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return defaultColor
}

func iconSVG(p *Props) string {
	name := strings.TrimSpace(p.Icon)
	if name == "" {
		name = defaultIcon
	}
	if svg, ok := core.IconSVG(name); ok {
		return svg
	}
	if svg, ok := core.IconSVG(defaultIcon); ok {
		return svg
	}
	return ""
}

//go:embed carticon.css
var cartIconCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	mode := effectiveMode(p)

	drawerLeft := ""
	if strings.TrimSpace(p.DrawerSide) == "left" {
		drawerLeft = "1"
	}

	alignLeft := ""
	if strings.TrimSpace(p.Align) == "left" {
		alignLeft = "1"
	}
	vars := map[string]string{
		"color":      effectiveColor(p),
		"dropdown":   "",
		"drawer":     "",
		"modal":      "",
		"hover":      "",
		"inline":     "",
		"drawerLeft": drawerLeft,
		"alignLeft":  alignLeft,
	}
	vars[mode] = "1"
	if err := core.ApplyComponentCSSTmpl(b, sel, cartIconCSS, vars); err != nil {

		panic(fmt.Sprintf("cartIcon 组件样式解析失败: %v", err))
	}

	b.AddHover(sel+" .sky-cart-icon-trigger:hover", []string{"filter: brightness(1.08)"})
	b.AddActive(sel+" .sky-cart-icon-trigger:active", []string{"filter: brightness(0.94)"})

	if mode == ModeHover {
		b.AddHover(sel+" .sky-cart-icon-hoverwrap:hover .sky-cart-icon-panel", []string{"display: block"})
	}

}

//go:embed enhance.js
var cartIconEnhanceJS string

//go:embed cart_icon.jet
var cartIconTemplate string

func init() {
	core.Register(Widget)
	core.RegisterTemplate("cart_icon", cartIconTemplate)
	core.RegisterTemplate("site_cart", siteCartTemplate)

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCartIconPanels"},
		Feats:  []string{"data-cart-icon"},
		Source: cartIconEnhanceJS,
	})
}

//go:embed site_cart.jet
var siteCartTemplate string
