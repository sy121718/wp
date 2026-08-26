// Package container 实现 core.container 标准容器组件（规范 docs/02-A §3）：
// 组件树的唯一结构载体，既可作为页面第一层顶级 Section，也可无限自由嵌套；
// 编译期直出单层原生 HTML 语义标签，不产生冗余 Wrapper。
package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.container"

// 布局引擎常量。
const (
	EngineFlex = "flex"
	EngineGrid = "grid"
)

var (
	// nodeIDRe 节点 ID 白名单：字母数字下划线连字符，1~64 位。
	nodeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// cssValueRe CSS 值白名单：字母数字与常见安全符号，禁止引号/分号/花括号/@/反斜杠/尖括号等注入载体。
	cssValueRe = regexp.MustCompile(`^[A-Za-z0-9#%.,()\-+/:?=&_~ ]*$`)
)

// allowedContainerTags 容器允许的原生语义标签。
var allowedContainerTags = map[string]bool{
	"div": true, "section": true, "article": true, "aside": true,
	"nav": true, "header": true, "footer": true,
}

// justifyMap 主轴对齐关键字到 CSS 值的映射。
var justifyMap = map[string]string{
	"start": "flex-start", "center": "center", "end": "flex-end",
	"between": "space-between", "around": "space-around", "evenly": "space-evenly",
}

// alignMap 交叉轴对齐关键字到 CSS 值的映射。
var alignMap = map[string]string{
	"start": "flex-start", "center": "center", "end": "flex-end",
	"stretch": "stretch", "baseline": "baseline",
}

// allowedFlexDirection Flex 主轴方向白名单。
var allowedFlexDirection = map[string]bool{
	"row": true, "row-reverse": true, "column": true, "column-reverse": true,
}

// allowedOverflow 溢出处理白名单。
var allowedOverflow = map[string]bool{
	"visible": true, "hidden": true, "scroll": true, "auto": true,
}

// allowedBorderStyle 边框线型白名单。
var allowedBorderStyle = map[string]bool{
	"solid": true, "dashed": true, "dotted": true, "double": true,
}

// shadowLevels 阴影级别到 CSS 值的映射。
var shadowLevels = map[string]string{
	"sm": "0 1px 3px rgba(0,0,0,0.12)",
	"md": "0 4px 12px rgba(0,0,0,0.12)",
	"lg": "0 10px 28px rgba(0,0,0,0.16)",
	"xl": "0 20px 48px rgba(0,0,0,0.2)",
}

// shadowUpgrade 悬浮反馈时加深的阴影级别。
var shadowUpgrade = map[string]string{"": "md", "sm": "md", "md": "lg", "lg": "xl", "xl": "xl"}

// allowedEntrance 入场动效白名单（纯 CSS 实现，默认关闭）。
var allowedEntrance = map[string]bool{
	"": true, "fade-in": true, "slide-up": true,
}

// Responsive 三端字符串值（如内边距、间距）。
type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// ResponsiveInt 三端整数值（如栅格列数）。
type ResponsiveInt struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

// Props 标准容器能力描述（规范 docs/02-A §3）。
type Props struct {
	// Tag 原生语义标签：div/section/article/aside/nav/header/footer。
	Tag         string          `json:"tag"`
	Layout      LayoutProps     `json:"layout"`
	Box         BoxProps        `json:"box"`
	Visual      VisualProps     `json:"visual"`
	Interaction InteractionProps `json:"interaction"`
}

// LayoutProps 双排版引擎（Flexbox / Grid）。
type LayoutProps struct {
	// Engine 排版引擎：flex / grid。
	Engine string     `json:"engine"`
	Flex   *FlexProps `json:"flex,omitempty"`
	Grid   *GridProps `json:"grid,omitempty"`
}

// FlexProps Flexbox 排版参数。
type FlexProps struct {
	// Direction 主轴方向：row / row-reverse / column / column-reverse。
	Direction string `json:"direction,omitempty"`
	// Justify 主轴对齐：start / center / end / between / around / evenly。
	Justify string `json:"justify,omitempty"`
	// Align 交叉轴对齐：start / center / end / stretch / baseline。
	Align string `json:"align,omitempty"`
	// Wrap 是否允许自动换行。
	Wrap bool `json:"wrap,omitempty"`
	// Gap 子元素间距（CSS 长度值）。
	Gap string `json:"gap,omitempty"`
}

// GridProps Grid 栅格参数。
type GridProps struct {
	// Columns 栅格列数（1~12），支持三端响应式降级。
	Columns ResponsiveInt `json:"columns,omitempty"`
	// ColumnGap 列间距。
	ColumnGap string `json:"columnGap,omitempty"`
	// RowGap 行间距。
	RowGap string `json:"rowGap,omitempty"`
}

// BoxProps 盒模型尺寸，三端独立。
type BoxProps struct {
	// Padding 内边距（CSS 简写值），三端独立。
	Padding Responsive `json:"padding,omitempty"`
	// Margin 外边距（CSS 简写值，含 auto 居中），三端独立。
	Margin Responsive `json:"margin,omitempty"`
	// MinHeight 最小高度。
	MinHeight string `json:"minHeight,omitempty"`
	// MaxHeight 最大高度。
	MaxHeight string `json:"maxHeight,omitempty"`
	// Overflow 内容溢出处理：visible / hidden / scroll / auto。
	Overflow string `json:"overflow,omitempty"`
}

// VisualProps 视觉装饰。
type VisualProps struct {
	BgColor    string `json:"bgColor,omitempty"`
	BgGradient string `json:"bgGradient,omitempty"` // 如 "linear-gradient(to right, #fff, #000)"
	BgImage    string `json:"bgImage,omitempty"`    // 背景图 URL
	// 边框三要素，需同时提供才生效。
	BorderWidth string `json:"borderWidth,omitempty"`
	BorderStyle string `json:"borderStyle,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	// Radius 圆角弧度。
	Radius string `json:"radius,omitempty"`
	// Shadow 阴影级别：sm / md / lg / xl。
	Shadow string `json:"shadow,omitempty"`
}

// InteractionProps 交互状态与动画。
type InteractionProps struct {
	// Sticky 滚动吸顶定位。
	Sticky bool `json:"sticky,omitempty"`
	// StickyTop 吸顶偏移（CSS 长度值），默认 0。
	StickyTop string `json:"stickyTop,omitempty"`
	// HoverLift 悬浮上浮反馈（卡片场景）。
	HoverLift bool `json:"hoverLift,omitempty"`
	// Entrance 入场微动："" 关闭（默认）/ fade-in / slide-up。
	Entrance string `json:"entrance,omitempty"`
}

// Container core.container 组件实现。
type Container struct{}

// Type 实现组件接口。
func (Container) Type() string { return Type }

// IsSafeCSSValue 校验 CSS 值是否在安全白名单内（长度上限 500）。导出供其他组件复用。
func IsSafeCSSValue(v string) bool {
	return len(v) <= 500 && cssValueRe.MatchString(v)
}

// Validate 校验容器节点及整棵子树。
func (Container) Validate(node *core.Node, ids map[string]bool) (err error) {
	if !nodeIDRe.MatchString(node.ID) {
		return fmt.Errorf("无效的节点 ID: %q", node.ID)
	}
	if ids[node.ID] {
		return fmt.Errorf("节点 ID 重复: %q", node.ID)
	}
	ids[node.ID] = true

	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err = validateProps(&p); err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	for i, child := range node.Children {
		if err = core.ValidateNode(child, ids); err != nil {
			return fmt.Errorf("节点 %s 子节点 %d: %w", node.ID, i, err)
		}
	}
	return nil
}

// Render 渲染容器：单层原生语义标签，样式全部编译进 CSS。
func (Container) Render(node *core.Node, topLevel bool, html *strings.Builder, css *core.CSSBuckets) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}

	cls := core.NodeClass(node.ID)
	if topLevel {
		cls += " " + core.SectionClass
	}
	html.WriteString("<")
	html.WriteString(p.Tag)
	html.WriteString(" class=\"")
	html.WriteString(cls)
	html.WriteString("\">")
	for _, child := range node.Children {
		if err = core.RenderNode(child, false, html, css); err != nil {
			return err
		}
	}
	html.WriteString("</")
	html.WriteString(p.Tag)
	html.WriteString(">")

	compileCSS(node.ID, &p, css)
	return nil
}

// compileCSS 编译容器样式规则到三端 bucket。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string

	// --- 布局与排版 ---
	switch p.Layout.Engine {
	case EngineFlex:
		desktop = append(desktop, "display: flex")
		if f := p.Layout.Flex; f != nil {
			if f.Direction != "" {
				desktop = append(desktop, "flex-direction: "+f.Direction)
			}
			if f.Justify != "" {
				desktop = append(desktop, "justify-content: "+justifyMap[f.Justify])
			}
			if f.Align != "" {
				desktop = append(desktop, "align-items: "+alignMap[f.Align])
			}
			if f.Wrap {
				desktop = append(desktop, "flex-wrap: wrap")
			}
			if f.Gap != "" {
				desktop = append(desktop, "gap: "+f.Gap)
			}
		}
	case EngineGrid:
		desktop = append(desktop, "display: grid")
		if g := p.Layout.Grid; g != nil {
			if g.Columns.Desktop > 0 {
				desktop = append(desktop, fmt.Sprintf("grid-template-columns: repeat(%d, 1fr)", g.Columns.Desktop))
			}
			if g.Columns.Tablet > 0 {
				tablet = append(tablet, fmt.Sprintf("grid-template-columns: repeat(%d, 1fr)", g.Columns.Tablet))
			}
			if g.Columns.Mobile > 0 {
				mobile = append(mobile, fmt.Sprintf("grid-template-columns: repeat(%d, 1fr)", g.Columns.Mobile))
			}
			if g.ColumnGap != "" {
				desktop = append(desktop, "column-gap: "+g.ColumnGap)
			}
			if g.RowGap != "" {
				desktop = append(desktop, "row-gap: "+g.RowGap)
			}
		}
	}

	// --- 盒模型（三端独立） ---
	if v := p.Box.Padding.Desktop; v != "" {
		desktop = append(desktop, "padding: "+v)
	}
	if v := p.Box.Padding.Tablet; v != "" {
		tablet = append(tablet, "padding: "+v)
	}
	if v := p.Box.Padding.Mobile; v != "" {
		mobile = append(mobile, "padding: "+v)
	}
	if v := p.Box.Margin.Desktop; v != "" {
		desktop = append(desktop, "margin: "+v)
	}
	if v := p.Box.Margin.Tablet; v != "" {
		tablet = append(tablet, "margin: "+v)
	}
	if v := p.Box.Margin.Mobile; v != "" {
		mobile = append(mobile, "margin: "+v)
	}
	if v := p.Box.MinHeight; v != "" {
		desktop = append(desktop, "min-height: "+v)
	}
	if v := p.Box.MaxHeight; v != "" {
		desktop = append(desktop, "max-height: "+v)
	}
	if v := p.Box.Overflow; v != "" {
		desktop = append(desktop, "overflow: "+v)
	}

	// --- 视觉装饰 ---
	if v := p.Visual.BgColor; v != "" {
		desktop = append(desktop, "background-color: "+v)
	}
	// 渐变优先于背景图。
	if v := p.Visual.BgGradient; v != "" {
		desktop = append(desktop, "background-image: "+v)
	} else if v := p.Visual.BgImage; v != "" {
		desktop = append(desktop, "background-image: url("+v+")")
	}
	if p.Visual.BorderStyle != "" {
		desktop = append(desktop, "border: "+p.Visual.BorderWidth+" "+p.Visual.BorderStyle+" "+p.Visual.BorderColor)
	}
	if v := p.Visual.Radius; v != "" {
		desktop = append(desktop, "border-radius: "+v)
	}
	if v, ok := shadowLevels[p.Visual.Shadow]; p.Visual.Shadow != "" && ok {
		desktop = append(desktop, "box-shadow: "+v)
	}

	// --- 交互状态与动画 ---
	if p.Interaction.Sticky {
		desktop = append(desktop, "position: sticky")
		top := p.Interaction.StickyTop
		if top == "" {
			top = "0"
		}
		desktop = append(desktop, "top: "+top)
	}
	if p.Interaction.HoverLift {
		// 过渡声明入基础规则，触发态入 :hover 规则。
		desktop = append(desktop, "transition: transform 0.25s ease, box-shadow 0.25s ease")
	}
	if e := p.Interaction.Entrance; e != "" {
		// backwards 而非 both：动画结束后释放终态，避免填充态压制 hover 的 transform。
		desktop = append(desktop, "animation: wp-"+e+" 0.6s ease backwards")
		b.NeedKeyframes("wp-" + e)
	}

	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointTablet, sel, tablet)
	b.Add(core.BreakpointMobile, sel, mobile)

	if p.Interaction.HoverLift {
		b.Add(core.BreakpointDesktop, sel+":hover", []string{
			"transform: translateY(-6px)",
			"box-shadow: " + shadowLevels[shadowUpgrade[p.Visual.Shadow]],
		})
	}
}

// validateProps 校验容器能力参数。
func validateProps(p *Props) (err error) {
	if !allowedContainerTags[p.Tag] {
		return fmt.Errorf("无效的语义标签: %q", p.Tag)
	}

	// 布局引擎。
	switch p.Layout.Engine {
	case EngineFlex:
		if p.Layout.Flex == nil {
			return errors.New("flex 引擎必须提供 flex 参数")
		}
		f := p.Layout.Flex
		if f.Direction != "" && !allowedFlexDirection[f.Direction] {
			return fmt.Errorf("无效的主轴方向: %q", f.Direction)
		}
		if f.Justify != "" {
			if _, ok := justifyMap[f.Justify]; !ok {
				return fmt.Errorf("无效的主轴对齐: %q", f.Justify)
			}
		}
		if f.Align != "" {
			if _, ok := alignMap[f.Align]; !ok {
				return fmt.Errorf("无效的交叉轴对齐: %q", f.Align)
			}
		}
		if f.Gap != "" && !IsSafeCSSValue(f.Gap) {
			return fmt.Errorf("无效的子元素间距: %q", f.Gap)
		}
	case EngineGrid:
		if p.Layout.Grid == nil {
			return errors.New("grid 引擎必须提供 grid 参数")
		}
		g := p.Layout.Grid
		if g.Columns.Desktop < 1 || g.Columns.Desktop > 12 {
			return fmt.Errorf("桌面端栅格列数必须在 1~12 之间: %d", g.Columns.Desktop)
		}
		for bp, n := range map[string]int{"tablet": g.Columns.Tablet, "mobile": g.Columns.Mobile} {
			if n != 0 && (n < 1 || n > 12) {
				return fmt.Errorf("%s 端栅格列数必须在 1~12 之间: %d", bp, n)
			}
		}
		if g.ColumnGap != "" && !IsSafeCSSValue(g.ColumnGap) {
			return fmt.Errorf("无效的列间距: %q", g.ColumnGap)
		}
		if g.RowGap != "" && !IsSafeCSSValue(g.RowGap) {
			return fmt.Errorf("无效的行间距: %q", g.RowGap)
		}
	default:
		return fmt.Errorf("无效的排版引擎: %q", p.Layout.Engine)
	}

	// 盒模型。
	for name, v := range map[string]string{
		"desktop 内边距": p.Box.Padding.Desktop,
		"tablet 内边距":  p.Box.Padding.Tablet,
		"mobile 内边距":  p.Box.Padding.Mobile,
		"desktop 外边距": p.Box.Margin.Desktop,
		"tablet 外边距":  p.Box.Margin.Tablet,
		"mobile 外边距":  p.Box.Margin.Mobile,
		"最小高度":        p.Box.MinHeight,
		"最大高度":        p.Box.MaxHeight,
	} {
		if v != "" && !IsSafeCSSValue(v) {
			return fmt.Errorf("无效的%s: %q", name, v)
		}
	}
	if p.Box.Overflow != "" && !allowedOverflow[p.Box.Overflow] {
		return fmt.Errorf("无效的溢出处理: %q", p.Box.Overflow)
	}

	// 视觉装饰。
	for _, item := range []struct{ name, v string }{
		{"背景颜色", p.Visual.BgColor},
		{"背景渐变", p.Visual.BgGradient},
		{"背景图", p.Visual.BgImage},
		{"边框粗细", p.Visual.BorderWidth},
		{"边框颜色", p.Visual.BorderColor},
		{"圆角", p.Visual.Radius},
	} {
		if item.v != "" && !IsSafeCSSValue(item.v) {
			return fmt.Errorf("无效的%s: %q", item.name, item.v)
		}
	}
	borderSet := p.Visual.BorderWidth != "" || p.Visual.BorderStyle != "" || p.Visual.BorderColor != ""
	if borderSet && (p.Visual.BorderWidth == "" || p.Visual.BorderStyle == "" || p.Visual.BorderColor == "") {
		return errors.New("边框需同时提供粗细、线型与颜色")
	}
	if p.Visual.BorderStyle != "" && !allowedBorderStyle[p.Visual.BorderStyle] {
		return fmt.Errorf("无效的边框线型: %q", p.Visual.BorderStyle)
	}
	if p.Visual.Shadow != "" {
		if _, ok := shadowLevels[p.Visual.Shadow]; !ok {
			return fmt.Errorf("无效的阴影级别: %q", p.Visual.Shadow)
		}
	}

	// 交互状态与动画。
	if p.Interaction.StickyTop != "" && !IsSafeCSSValue(p.Interaction.StickyTop) {
		return fmt.Errorf("无效的吸顶偏移: %q", p.Interaction.StickyTop)
	}
	if !allowedEntrance[p.Interaction.Entrance] {
		return fmt.Errorf("无效的入场动效: %q", p.Interaction.Entrance)
	}
	return nil
}

// init 注册容器组件到编译内核。
func init() {
	core.Register(Container{})
}