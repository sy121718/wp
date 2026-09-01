// Package container 实现 core.container 标准容器组件（规范 docs/02-A §3）：
// 组件树的唯一结构载体，既可作为页面第一层顶级 Section，也可无限自由嵌套；
// 编译期直出单层原生 HTML 语义标签，不产生冗余 Wrapper。
package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
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
	"nav": true, "header": true, "footer": true, "main": true,
}

// justifyMap 主轴对齐关键字到 CSS 值的映射。
// 同时接受 CSS 标准值（检查器提交）与旧简写（兼容历史文档）。
var justifyMap = map[string]string{
	// CSS 标准值直通。
	"flex-start": "flex-start", "center": "center", "flex-end": "flex-end",
	"space-between": "space-between", "space-around": "space-around", "space-evenly": "space-evenly",
	// 旧简写兼容。
	"start": "flex-start", "end": "flex-end",
	"between": "space-between", "around": "space-around", "evenly": "space-evenly",
}

// alignMap 交叉轴对齐关键字到 CSS 值的映射。
// 同时接受 CSS 标准值与旧简写（兼容历史文档）。
var alignMap = map[string]string{
	"stretch": "stretch", "center": "center", "baseline": "baseline",
	"flex-start": "flex-start", "flex-end": "flex-end",
	"start": "flex-start", "end": "flex-end",
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

// Props 标准容器能力描述（规范 docs/02-A §3 + docs/03-A 面板能力）。
type Props struct {
	// Tag 原生语义标签：div/section/article/aside/nav/header/footer/main。
	Tag         string           `json:"tag"`
	Layout      LayoutProps      `json:"layout"`
	Box         BoxProps         `json:"box"`
	Visual      VisualProps      `json:"visual"`
	Interaction InteractionProps `json:"interaction"`
	// Position 定位系统（03-A §3.1 Tab1）：static/relative/absolute/sticky/drawer。
	Position PositionProps `json:"position,omitempty"`
	// StyleEx 样式扩展（03-A §3.1 Tab2）：背景双态/遮罩/形状分隔线/顺序/组父联动/属性。
	StyleEx StyleExProps `json:"styleEx,omitempty"`
}

// PositionProps 定位系统。
type PositionProps struct {
	// Type 定位类型：static（默认）/ relative / absolute / sticky / drawer。
	Type string `json:"type,omitempty"`
	// Top/Right/Bottom/Left absolute 精准坐标（CSS 长度值）。
	Top    string `json:"top,omitempty"`
	Right  string `json:"right,omitempty"`
	Bottom string `json:"bottom,omitempty"`
	Left   string `json:"left,omitempty"`
	// DrawerSide drawer 抽屉滑出边：left / right / bottom。
	DrawerSide string `json:"drawerSide,omitempty"`
	// DrawerOverlay 抽屉遮罩（:target 显隐，零 JS）。
	DrawerOverlay bool `json:"drawerOverlay,omitempty"`
	// DrawerTriggerID 唯一触发元素 ID（配合 button 等触发协议）。
	DrawerTriggerID string `json:"drawerTriggerId,omitempty"`
}

// StyleExProps 样式扩展。
type StyleExProps struct {
	// BackgroundHover 悬停背景（纯色/渐变；继承 Background 的缺省语义）。
	BackgroundHover string `json:"backgroundHover,omitempty"`
	// Overlay 背景覆盖层（纯色/渐变半透明遮罩，保障文本可读）。
	Overlay string `json:"overlay,omitempty"`
	// ShapeDivider 形状分隔线：wave / slant / curve；空=关闭。
	ShapeDivider string `json:"shapeDivider,omitempty"`
	// ShapeDividerPosition 形状位置：top / bottom（默认 bottom）。
	ShapeDividerPosition string `json:"shapeDividerPosition,omitempty"`
	// Order 子项顺序（flex/grid 中 -1~99）。
	Order int `json:"order,omitempty"`
	// GroupParent 父子悬停联动：父容器 hover 时子组件可触发联动样式。
	GroupParent bool `json:"groupParent,omitempty"`
	// Attributes 自定义 HTML 键值对（白名单 key + 安全 value）。
	Attributes []AttributeKV `json:"attributes,omitempty"`
}

// AttributeKV 自定义属性键值对。
type AttributeKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
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
	// Justify 主轴对齐：CSS 标准值 flex-start / center / flex-end /
	// space-between / space-around / space-evenly（旧简写 start/end/between/around/evenly 兼容）。
	Justify string `json:"justify,omitempty"`
	// Align 交叉轴对齐：CSS 标准值 stretch / center / baseline /
	// flex-start / flex-end（旧简写 start/end 兼容）。
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
	BgImage    string `json:"bgImage,omitempty"`    // 背景图 URL（媒体库选择回填；画布/产物直出）
	// BgPosition 背景定位（default=浏览器默认）：center / left top 等关键词组合；custom 时取 BgPositionXY。
	BgPosition string `json:"bgPosition,omitempty" ct:"select,default=（默认）,custom=自定义,center=居中,center top=中上,center bottom=中下,left top=左上,left center=左中,left bottom=左下,right top=右上,right center=右中,right bottom=右下,default=default,sec=style,label=背景定位"`
	// BgPositionXY 自定义定位值（BgPosition=custom 时生效），如 "50% 20%"。
	BgPositionXY string `json:"bgPositionXY,omitempty" ct:"safe,maxlen=40,sec=style"`
	// BgAttachment 背景附着方式：default / scroll / fixed / local。
	BgAttachment string `json:"bgAttachment,omitempty" ct:"select,default=（默认）,scroll=随页面滚动,fixed=固定（视差）,local=随内容滚动,default=default,sec=style,label=背景附着方式"`
	// BgRepeat 背景重复：default / no-repeat / repeat / repeat-x / repeat-y。
	BgRepeat string `json:"bgRepeat,omitempty" ct:"select,default=（默认）,no-repeat=不重复,repeat=平铺,repeat-x=横向平铺,repeat-y=纵向平铺,default=default,sec=style,label=背景重复"`
	// BgSize 显示尺寸：default / auto / contain / cover / custom（取 BgSizeValue）。
	BgSize string `json:"bgSize,omitempty" ct:"select,default=（默认）,auto=原始,contain=完整包含,cover=铺满裁剪,custom=自定义,default=default,sec=style,label=显示尺寸"`
	// BgSizeValue 自定义尺寸值（BgSize=custom 时生效），如 "100% auto"。
	BgSizeValue string `json:"bgSizeValue,omitempty" ct:"safe,maxlen=40,sec=style"`
	// 边框三要素，需同时提供才生效。
	BorderWidth string `json:"borderWidth,omitempty"`
	BorderStyle string `json:"borderStyle,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	// Radius 圆角弧度。
	Radius string `json:"radius,omitempty"`
	// Shadow 阴影级别：sm / md / lg / xl；custom 时取 ShadowCustom。
	Shadow string `json:"shadow,omitempty"`
	// ShadowCustom 自定义阴影四参 + 颜色（Shadow=custom 时生效）。
	ShadowX      string `json:"shadowX,omitempty" ct:"dimension,maxlen=20,sec=style,label=阴影 X"`
	ShadowY      string `json:"shadowY,omitempty" ct:"dimension,maxlen=20,sec=style,label=阴影 Y"`
	ShadowBlur   string `json:"shadowBlur,omitempty" ct:"dimension,maxlen=20,sec=style,label=阴影模糊"`
	ShadowSpread string `json:"shadowSpread,omitempty" ct:"dimension,maxlen=20,sec=style,label=阴影扩散"`
	ShadowColor  string `json:"shadowColor,omitempty" ct:"color,maxlen=200,sec=style,label=阴影颜色"`
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

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（声明式控件）。
func (Container) PropsSpec() any { return &Props{} }

// IsSafeCSSValue 校验 CSS 值是否在安全白名单内（长度上限 500）。导出供其他组件复用。
func IsSafeCSSValue(v string) bool {
	return len(v) <= 500 && cssValueRe.MatchString(v)
}

// shapeDividers 形状分隔线白名单（03-A §3.1 Tab2，纯 SVG 装饰）。
var shapeDividers = map[string]string{
	"wave":  `<svg viewBox="0 0 1440 64" preserveAspectRatio="none" aria-hidden="true"><path fill="currentColor" d="M0 32c120-32 240-32 360 0s240 32 360 0 240-32 360 0 240 32 360 0v64H0z"/></svg>`,
	"slant": `<svg viewBox="0 0 1440 64" preserveAspectRatio="none" aria-hidden="true"><path fill="currentColor" d="M0 64L1440 0v64H0z"/></svg>`,
	"curve": `<svg viewBox="0 0 1440 64" preserveAspectRatio="none" aria-hidden="true"><path fill="currentColor" d="M0 64C360 0 1080 0 1440 64H0z"/></svg>`,
}

// attrKeyRe 自定义属性 key 白名单（data-* / aria-* / 常见属性）。
var attrKeyRe = regexp.MustCompile(`^(data-[a-z0-9-]{1,32}|aria-[a-z0-9-]{1,32}|role|title|tabindex)$`)

// attrValueSafe 属性值白名单：允许中文与常见符号，禁引号/尖括号/花括号/@（防属性逃逸）。
func attrValueSafe(v string) bool {
	if len(v) > 200 {
		return false
	}
	for _, r := range v {
		if r == '"' || r == '\'' || r == '<' || r == '>' || r == '\\' || r == '`' {
			return false
		}
	}
	return true
}

// Validate 校验容器节点及整棵子树。
func (Container) Validate(node *core.Node, ids map[string]bool) (err error) {
	if !nodeIDRe.MatchString(node.ID) {
		return fmt.Errorf("无效的节点 ID: %q", node.ID)
	}
	if err = core.ValidateNodeName(node.Name); err != nil {
		return err
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
func (Container) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
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

	var attrs strings.Builder
	// 组父联动标记（03-A：子组件可经 [data-wp-group] 联动）。
	if p.StyleEx.GroupParent {
		attrs.WriteString(` data-wp-group="true"`)
	}
	// 自定义属性键值对（白名单 key + 安全 value）。
	for _, kv := range p.StyleEx.Attributes {
		attrs.WriteString(" ")
		attrs.WriteString(kv.Key)
		attrs.WriteString(`="`)
		attrs.WriteString(html.EscapeString(kv.Value))
		attrs.WriteString(`"`)
	}
	// 抽屉协议（:target 显隐，零 JS）。
	if p.Position.Type == "drawer" {
		attrs.WriteString(` id="wp-drawer-`)
		attrs.WriteString(node.ID)
		attrs.WriteString(`" data-drawer-side="`)
		attrs.WriteString(p.Position.DrawerSide)
		attrs.WriteString(`"`)
		if p.Position.DrawerOverlay {
			attrs.WriteString(` data-drawer-overlay="true"`)
		}
	}

	ctx.HTML.WriteString("<")
	ctx.HTML.WriteString(p.Tag)
	ctx.HTML.WriteString(" class=\"")
	ctx.HTML.WriteString(cls)
	ctx.HTML.WriteString("\"")
	ctx.HTML.WriteString(attrs.String())
	ctx.HTML.WriteString(">")

	// 形状分隔线（顶部）：内嵌纯 SVG 装饰。
	if p.StyleEx.ShapeDivider != "" && p.StyleEx.ShapeDividerPosition == "top" {
		ctx.HTML.WriteString(`<span class="wp-shape wp-shape-top">`)
		ctx.HTML.WriteString(shapeDividers[p.StyleEx.ShapeDivider])
		ctx.HTML.WriteString(`</span>`)
	}

	for _, child := range node.Children {
		if err = core.RenderNode(child, false, ctx); err != nil {
			return err
		}
	}

	if p.StyleEx.ShapeDivider != "" && p.StyleEx.ShapeDividerPosition != "top" {
		ctx.HTML.WriteString(`<span class="wp-shape wp-shape-bottom">`)
		ctx.HTML.WriteString(shapeDividers[p.StyleEx.ShapeDivider])
		ctx.HTML.WriteString(`</span>`)
	}

	ctx.HTML.WriteString("</")
	ctx.HTML.WriteString(p.Tag)
	ctx.HTML.WriteString(">")

	compileCSS(node.ID, &p, ctx.CSS)
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
		// 背景显示控制（对齐 Elementor：定位/附着/重复/尺寸），仅背景图存在时输出。
		if v := p.Visual.BgPosition; v != "" && v != "default" {
			if v == "custom" && p.Visual.BgPositionXY != "" {
				desktop = append(desktop, "background-position: "+p.Visual.BgPositionXY)
			} else if v != "custom" {
				desktop = append(desktop, "background-position: "+v)
			}
		}
		if v := p.Visual.BgAttachment; v != "" && v != "default" {
			desktop = append(desktop, "background-attachment: "+v)
		}
		if v := p.Visual.BgRepeat; v != "" && v != "default" {
			desktop = append(desktop, "background-repeat: "+v)
		}
		if v := p.Visual.BgSize; v != "" && v != "default" {
			if v == "custom" && p.Visual.BgSizeValue != "" {
				desktop = append(desktop, "background-size: "+p.Visual.BgSizeValue)
			} else if v != "custom" {
				desktop = append(desktop, "background-size: "+v)
			}
		}
	}
	if p.Visual.BorderStyle != "" {
		desktop = append(desktop, "border: "+p.Visual.BorderWidth+" "+p.Visual.BorderStyle+" "+p.Visual.BorderColor)
	}
	if v := p.Visual.Radius; v != "" {
		desktop = append(desktop, "border-radius: "+v)
	}
	if p.Visual.Shadow == "custom" {
		x, y, blur, spread := p.Visual.ShadowX, p.Visual.ShadowY, p.Visual.ShadowBlur, p.Visual.ShadowSpread
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "4px"
		}
		if blur == "" {
			blur = "12px"
		}
		if spread == "" {
			spread = "0"
		}
		color := p.Visual.ShadowColor
		if color == "" {
			color = "rgba(0,0,0,.12)"
		}
		desktop = append(desktop, "box-shadow: "+x+" "+y+" "+blur+" "+spread+" "+color)
	} else if v, ok := shadowLevels[p.Visual.Shadow]; p.Visual.Shadow != "" && ok {
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

	// --- 定位系统（03-A） ---
	switch p.Position.Type {
	case "relative":
		b.Add(core.BreakpointDesktop, sel, []string{"position: relative"})
	case "absolute":
		abs := []string{"position: absolute"}
		if p.Position.Top != "" {
			abs = append(abs, "top: "+p.Position.Top)
		}
		if p.Position.Right != "" {
			abs = append(abs, "right: "+p.Position.Right)
		}
		if p.Position.Bottom != "" {
			abs = append(abs, "bottom: "+p.Position.Bottom)
		}
		if p.Position.Left != "" {
			abs = append(abs, "left: "+p.Position.Left)
		}
		b.Add(core.BreakpointDesktop, sel, abs)
	case "sticky":
		b.Add(core.BreakpointDesktop, sel, []string{"position: sticky", "top: 0"})
	case "drawer":
		// 抽屉：fixed + 移出视口，:target 滑入（触发协议 `href="#wp-drawer-<id>"`，零 JS）。
		var drawer []string
		drawer = append(drawer, "position: fixed", "transition: transform 0.3s ease", "z-index: 900")
		switch p.Position.DrawerSide {
		case "left":
			drawer = append(drawer, "left: 0", "top: 0", "bottom: 0", "width: 300px", "transform: translateX(-100%)")
		case "right":
			drawer = append(drawer, "right: 0", "top: 0", "bottom: 0", "width: 300px", "transform: translateX(100%)")
		case "bottom":
			drawer = append(drawer, "left: 0", "right: 0", "bottom: 0", "transform: translateY(100%)")
		}
		b.Add(core.BreakpointDesktop, sel, drawer)
		b.Add(core.BreakpointDesktop, sel+":target", []string{"transform: none"})
	}

	// --- 样式扩展（03-A） ---
	if p.StyleEx.Order != 0 {
		b.Add(core.BreakpointDesktop, sel, []string{fmt.Sprintf("order: %d", p.StyleEx.Order)})
	}
	if p.StyleEx.BackgroundHover != "" {
		b.Add(core.BreakpointDesktop, sel, []string{"transition: background 0.2s ease"})
		b.Add(core.BreakpointDesktop, sel+":hover", []string{"background: " + p.StyleEx.BackgroundHover})
	}
	if p.StyleEx.Overlay != "" {
		if p.Position.Type == "static" {
			b.Add(core.BreakpointDesktop, sel, []string{"position: relative"})
		}
		b.Add(core.BreakpointDesktop, sel+"::before", []string{
			"content: \"\"",
			"position: absolute",
			"inset: 0",
			"pointer-events: none",
			"background: " + p.StyleEx.Overlay,
			"z-index: 0",
		})
		b.Add(core.BreakpointDesktop, sel+" > *", []string{"position: relative", "z-index: 1"})
	}
	// 形状分隔线样式。
	if p.StyleEx.ShapeDivider != "" {
		shapePos := p.StyleEx.ShapeDividerPosition
		if shapePos == "" {
			shapePos = "bottom"
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-shape", []string{
			"position: absolute",
			"left: 0", "right: 0",
			"height: 48px",
			"line-height: 0",
			"z-index: 2",
			"pointer-events: none",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-shape svg", []string{"width: 100%", "height: 100%", "display: block"})
		b.Add(core.BreakpointDesktop, sel+" .wp-shape-"+shapePos, []string{shapePos + ": 0"})
		b.Add(core.BreakpointDesktop, sel+" .wp-shape svg", []string{"color: " + shapeColor(p)})
		if p.Position.Type == "static" {
			b.Add(core.BreakpointDesktop, sel, []string{"position: relative"})
		}
	}
}

// shapeColor 形状分隔线颜色（跟随容器背景反色缺省：当前色）。
func shapeColor(p *Props) string {
	if p.Visual.BgColor != "" {
		return p.Visual.BgColor
	}
	return "currentColor"
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
		{"背景自定义定位", p.Visual.BgPositionXY},
		{"阴影 X", p.Visual.ShadowX},
		{"阴影 Y", p.Visual.ShadowY},
		{"阴影模糊", p.Visual.ShadowBlur},
		{"阴影扩散", p.Visual.ShadowSpread},
		{"阴影颜色", p.Visual.ShadowColor},
		{"背景自定义尺寸", p.Visual.BgSizeValue},
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
	if p.Visual.Shadow != "" && p.Visual.Shadow != "custom" {
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

	// 定位系统（03-A）。
	switch p.Position.Type {
	case "", "static":
	case "relative":
	case "absolute":
		// 绝对定位至少一个坐标。
		if p.Position.Top == "" && p.Position.Right == "" && p.Position.Bottom == "" && p.Position.Left == "" {
			return fmt.Errorf("绝对定位必须提供至少一个坐标（top/right/bottom/left）")
		}
	case "sticky":
		// 与 Interaction.Sticky 兼容并存。
	case "drawer":
		if p.Position.DrawerSide == "" {
			return fmt.Errorf("drawer 定位必须提供滑出方向（left/right/bottom）")
		}
		if p.Position.DrawerSide != "left" && p.Position.DrawerSide != "right" && p.Position.DrawerSide != "bottom" {
			return fmt.Errorf("无效的抽屉方向: %q", p.Position.DrawerSide)
		}
		if !nodeIDRe.MatchString(p.Position.DrawerTriggerID) {
			return fmt.Errorf("抽屉必须提供唯一触发 ID")
		}
	default:
		return fmt.Errorf("无效的定位类型: %q", p.Position.Type)
	}
	for name, v := range map[string]string{
		"top 坐标": p.Position.Top, "right 坐标": p.Position.Right,
		"bottom 坐标": p.Position.Bottom, "left 坐标": p.Position.Left,
	} {
		if v != "" && !IsSafeCSSValue(v) {
			return fmt.Errorf("无效的%s: %q", name, v)
		}
	}

	// 样式扩展（03-A）。
	if p.StyleEx.BackgroundHover != "" && !IsSafeCSSValue(p.StyleEx.BackgroundHover) {
		return fmt.Errorf("无效的悬停背景: %q", p.StyleEx.BackgroundHover)
	}
	if p.StyleEx.Overlay != "" && !IsSafeCSSValue(p.StyleEx.Overlay) {
		return fmt.Errorf("无效的背景覆盖层: %q", p.StyleEx.Overlay)
	}
	if p.StyleEx.ShapeDivider != "" {
		if _, ok := shapeDividers[p.StyleEx.ShapeDivider]; !ok {
			return fmt.Errorf("无效的形状分隔线: %q（仅 wave/slant/curve）", p.StyleEx.ShapeDivider)
		}
	}
	if p.StyleEx.ShapeDividerPosition != "" && p.StyleEx.ShapeDividerPosition != "top" && p.StyleEx.ShapeDividerPosition != "bottom" {
		return fmt.Errorf("形状分隔线位置仅支持 top/bottom: %q", p.StyleEx.ShapeDividerPosition)
	}
	if p.StyleEx.Order < -1 || p.StyleEx.Order > 99 {
		return fmt.Errorf("顺序值必须在 -1~99 之间: %d", p.StyleEx.Order)
	}
	for _, kv := range p.StyleEx.Attributes {
		if !attrKeyRe.MatchString(kv.Key) {
			return fmt.Errorf("无效的自定义属性 key: %q（仅 data-*/aria-*/role/title/tabindex）", kv.Key)
		}
		if !attrValueSafe(kv.Value) {
			return fmt.Errorf("无效的自定义属性 value: %q", kv.Value)
		}
	}
	return nil
}

// init 注册容器组件到编译内核。
func init() {
	core.Register(Container{})
}
