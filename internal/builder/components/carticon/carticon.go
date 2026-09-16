// Package carticon 实现 core.cartIcon 购物车悬浮组件。
//
// 它解决的是「访客怎么找到购物车」：一个图标，点开就能看到车里有什么、改数量、去结算 ——
// 不用离开当前页面。四种形态（下拉浮层 / 侧边抽屉 / 居中弹窗 / 悬停浮层）共用同一份内容：
// **片段现拉的 cartView**，绝不是构建期烘进产物的静态列表。
//
// 内容必须实时：购物车是每个访客各不相同的动态状态，烘进静态产物等于把某一个人的车
// 发布给所有人。所以产物里只有图标与外壳，内容由 /_fragments/cartView 现取。
//
// 无 JS 时：<details> 仍能原生展开，但片段拉不进来 —— 因此浮层里**始终**留一个指向
// 购物车页（槽位 cart）的链接兜底；槽位没配时图标退化为不可点的展示件（不猜路径）。
package carticon

import (
	_ "embed" // carticon.css / cart_icon.jet / enhance.js 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.cartIcon"

// 展示形态。
const (
	// ModeDropdown 下拉浮层：图标下方（或上方）浮出一块。
	ModeDropdown = "dropdown"
	// ModeDrawer 侧边抽屉：从一侧滑出，占满纵向。
	ModeDrawer = "drawer"
	// ModeModal 居中弹窗：整屏遮罩 + 居中面板。
	ModeModal = "modal"
	// ModeHover 悬停浮层：鼠标移上即出。触屏没有悬停，同一形态改由「点击展开」接管
	//（sr-only checkbox + 覆盖层 label，见 carticon.css 的 @hovernone 块）。
	ModeHover = "hover"
)

// 片段路径（与 runtimefragment 的注册一致，改一处必须改两处）。
const (
	cartViewPath    = "/_fragments/cartView"
	cartSummaryPath = "/_fragments/cartSummary"
)

const (
	defaultIcon  = "shopping-cart"
	defaultLabel = "购物车"
	// defaultColor 图标与角标的主色（主题 Token + 兜底，与 button/badge 同一取色约定）。
	defaultColor = "var(--sky-c-primary, #2563eb)"
)

// Props 购物车图标属性。
type Props struct {
	// Mode 展示形态。
	Mode string `json:"mode,omitempty" ct:"select,dropdown=下拉浮层,drawer=侧边抽屉,modal=居中弹窗,hover=悬停浮层,default=dropdown,sec=content,label=展示形态"`
	// Align 下拉浮层的贴边（dropdown 形态）。
	Align string `json:"align,omitempty" ct:"select,right=右对齐,left=左对齐,default=right,sec=style,label=浮层对齐"`
	// DrawerSide 抽屉贴哪一侧（drawer 形态）。
	DrawerSide string `json:"drawerSide,omitempty" ct:"select,right=右侧,left=左侧,default=right,sec=style,label=抽屉方向"`
	// Icon 图标名（core 内置图标库，如 shopping-cart / shopping-basket）。
	Icon string `json:"icon,omitempty" ct:"text,maxlen=40,sec=style,label=图标名"`
	// Label 标签文字（同时是无障碍标签；留空用「购物车」）。
	Label string `json:"label,omitempty" ct:"text,maxlen=20,sec=content,label=文字"`
	// ShowLabel 是否在图标旁显示文字（默认只显示图标）。
	ShowLabel bool `json:"showLabel,omitempty" ct:"bool,sec=content,label=显示文字"`
	// ShowCount 是否显示件数角标（由 cartSummary 片段现取）。
	ShowCount bool `json:"showCount,omitempty" ct:"bool,sec=content,label=件数角标"`
	// Color 图标与角标主色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
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
		// 标签文字是作者填的文案，参与内容翻译。
		Translatable: []string{"label"},
	},
}

// effectiveMode 有效形态（未知值兜底下拉浮层）。
func effectiveMode(p *Props) string {
	switch p.Mode {
	case ModeDrawer, ModeModal, ModeHover:
		return p.Mode
	default:
		return ModeDropdown
	}
}

// effectiveLabel 有效标签文字。
func effectiveLabel(p *Props) string {
	if l := strings.TrimSpace(p.Label); l != "" {
		return l
	}
	return defaultLabel
}

// effectiveColor 有效主色。
func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return defaultColor
}

// iconSVG 取内置图标；未知名称回退默认（图标名写错不该让整页构建失败，
// 但也不能静默输出空 —— 回退到购物车图标至少还是个购物车入口）。
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

// compileCSS 购物车图标样式（形态差异由规则级 @if 控制）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	mode := effectiveMode(p)
	// 抽屉方向：左 / 右各一组规则。
	drawerLeft := ""
	if strings.TrimSpace(p.DrawerSide) == "left" {
		drawerLeft = "1"
	}
	// 下拉贴边：默认右对齐。
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
		"drawerLeft": drawerLeft,
		"alignLeft":  alignLeft,
	}
	vars[mode] = "1"
	if err := core.ApplyComponentCSSTmpl(b, sel, cartIconCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷：静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("cartIcon 组件样式解析失败: %v", err))
	}
	// 悬停态（图标底衬）走 bucket API：hover 的规则必须包在 @media (hover: hover) 里，
	// 而样式源只支持 max-width 断点。按压反馈用 AddActive（不带媒体查询，触屏也有）。
	b.AddHover(sel+" .sky-cart-icon-trigger:hover", []string{"filter: brightness(1.08)"})
	b.AddActive(sel+" .sky-cart-icon-trigger:active", []string{"filter: brightness(0.94)"})
	// 悬停展开浮层：只有 hover 形态需要，且必须包在 @media (hover: hover) 里
	//（触屏上「点一下卡住悬停态」是移动端最常见的粘滞 bug）。
	// 注意这两个 API 只负责包媒体查询与进桶，**不替你加伪类** —— 选择器要自带 :hover。
	// 触屏那半边在样式源里：carticon.css 的 @hovernone 块产出 @media (hover: none)，
	// 由 checkbox 的 :checked 驱动展开（两个媒体查询互斥，各管一种输入方式）。
	if mode == ModeHover {
		b.AddHover(sel+" .sky-cart-icon-hoverwrap:hover .sky-cart-icon-panel", []string{"display: block"})
	}
	// 购物车片段内容的基础样式（幂等：两个组件同时用也只出一份）。
	AddCartFragmentCSS(b)
}

//go:embed enhance.js
var cartIconEnhanceJS string

//go:embed cart_icon.jet
var cartIconTemplate string

// init 注册购物车图标组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("cart_icon", cartIconTemplate)
	// 点遮罩 / 按 Esc 关闭浮层：<details> 原生只支持点 summary 收起，点外部不关。
	// 十几行补上这个差距，不自己实现整套开合（收起仍由原生 details 负责）。
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCartIconPanels"},
		Feats:  []string{"data-cart-icon-panel"},
		Source: cartIconEnhanceJS,
	})
}
