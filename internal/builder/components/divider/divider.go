// Package divider 实现 core.divider 分割线组件（规范《02-C6 分割线组件规范》）。
//
// 极轻量装饰性原子组件：区块/列表/表单内水平视觉分隔，支持嵌入微型文本或图标
// （"或者"、OR、品牌小标志）。纯线输出单层原生 <hr>；嵌入场景输出紧凑 Flex 结构
// （两侧线 + 中间元素），零客户端 JS。基础盒模型与通用样式继承自 02-C0。
package divider

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.divider"

// 线条类型。
const (
	LineSolid  = "solid"
	LineDashed = "dashed"
	LineDotted = "dotted"
	LineDouble = "double"
)

// 嵌入类型。
const (
	InsetNone = "none"
	InsetText = "text"
	InsetIcon = "icon"
)

// 嵌入位置（元素在线条上的落点）。
const (
	PosCenter = "center"
	PosLeft   = "left"
	PosRight  = "right"
)

// builtinInsetIcons 内置白名单微图标（24 viewBox，stroke=currentColor，装饰性）。
var builtinInsetIcons = map[string]string{
	"star":    `<path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" stroke-linejoin="round"/>`,
	"diamond": `<path d="M12 3l9 9-9 9-9-9 9-9z" stroke-linejoin="round"/>`,
	"dot":     `<circle cx="12" cy="12" r="3"/>`,
}

// Width 三端总宽度。
type Width struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// Inset 嵌入元素配置。
type Inset struct {
	// Kind 嵌入类型：none / text / icon（默认 none）。
	Kind string `json:"kind,omitempty" ct:"select,none=纯线,text=嵌入文本,icon=嵌入图标,default=none,sec=content,label=样式"`
	// Text 嵌入文本。
	Text string `json:"text,omitempty" ct:"text,maxlen=50,sec=content"`
	// FontSize / FontWeight / Color 嵌入文本样式。
	FontSize   string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style"`
	FontWeight string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,sec=style,label=字重"`
	Color      string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`
	// IconName 内置图标（kind=icon）。
	IconName string `json:"iconName,omitempty" ct:"select,star=星形,diamond=菱形,dot=圆点,sec=content,label=图标样式"`
	// Position 嵌入位置：center / left / right（默认 center）。
	Position string `json:"position,omitempty" ct:"select,center=居中,left=靠左,right=靠右,default=center,sec=content,label=位置"`
	// Spacing 元素与两侧线条的留白。
	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style"`
}

// Props divider 属性。
type Props struct {
	// Style 线条类型：solid / dashed / dotted / double（默认 solid）。
	Style string `json:"style,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,default=solid,sec=style,label=线型"`
	// Weight 线粗（height），如 "1px" / "2px"。
	Weight string `json:"weight,omitempty" ct:"dimension,maxlen=20,sec=style"`
	// Width 总宽度三端（百分比或固定像素；空=100%）。
	Width Width `json:"width,omitempty"`
	// Align 对齐（宽度非 100% 时）：left / center / right（默认 center）。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`
	// Color 线条颜色或主题 Token。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`
	// Inset 嵌入元素。
	Inset Inset `json:"inset,omitempty"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：嵌入配置与样式取值。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Weight != "" && !core.IsSafeCSSValue(p.Weight) {
		return fmt.Errorf("无效的线粗: %q", p.Weight)
	}
	if p.Color != "" && !core.IsSafeCSSValue(p.Color) {
		return fmt.Errorf("无效的线条颜色: %q", p.Color)
	}
	for bp, v := range map[string]string{"desktop": p.Width.Desktop, "tablet": p.Width.Tablet, "mobile": p.Width.Mobile} {
		if v != "" && !core.IsSafeCSSValue(v) {
			return fmt.Errorf("无效的 %s 端宽度: %q", bp, v)
		}
	}
	if p.Inset.Kind == InsetText && p.Inset.Text == "" {
		return fmt.Errorf("文本嵌入必须提供文案")
	}
	if p.Inset.Kind == InsetIcon {
		if _, ok := builtinInsetIcons[p.Inset.IconName]; !ok {
			return fmt.Errorf("无效的内置图标: %q（仅 star/diamond/dot）", p.Inset.IconName)
		}
	}
	if p.Inset.Spacing != "" && !core.IsSafeCSSValue(p.Inset.Spacing) {
		return fmt.Errorf("无效的嵌入间距: %q", p.Inset.Spacing)
	}
	if p.Inset.FontSize != "" && !core.IsSafeCSSValue(p.Inset.FontSize) {
		return fmt.Errorf("无效的嵌入字号: %q", p.Inset.FontSize)
	}
	if p.Inset.Color != "" && !core.IsSafeCSSValue(p.Inset.Color) {
		return fmt.Errorf("无效的嵌入颜色: %q", p.Inset.Color)
	}
	return nil
}

// compileCSS 线条/嵌入/对齐/宽度三端样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	lineDecl := func() string {
		style := p.Style
		if style == "" {
			style = LineSolid
		}
		weight := p.Weight
		if weight == "" {
			weight = "1px"
		}
		// 使用 border-top 绘制（.dt-line 及 hr）。
		borderStyle := style
		if style == LineDouble {
			borderStyle = "double"
			if weight == "1px" {
				weight = "3px" // 双实线最小辨识度
			}
		}
		return core.CSSDecl("border-top", weight, borderStyle, lineColor(p))
	}

	// 无嵌入：hr 直用线样式。
	if p.Inset.Kind == InsetNone || p.Inset.Kind == "" {
		var decls []string
		if color := lineColor(p); color != "" {
			decls = append(decls, core.CSSDecl("border-top", strWeight(p), p.StyleOr(LineSolid), color))
		} else {
			decls = append(decls, core.CSSDecl("border-top", strWeight(p), p.StyleOr(LineSolid)))
		}
		decls = append(decls, "margin: 0")
		b.Add(core.BreakpointDesktop, sel, decls)
	} else {
		// 有嵌入：flex 容器 + 两段线。
		b.Add(core.BreakpointDesktop, sel, []string{
			"display: flex",
			"align-items: center",
			"width: 100%",
		})
		// 线段基础（flex 比例由 position 决定：center 等分，left 左短右长，right 反之）。
		lineBase := []string{"height: 0", lineDecl()}
		var leftFlex, rightFlex string
		switch p.Inset.Position {
		case PosLeft:
			leftFlex, rightFlex = "0.5", "1.5"
		case PosRight:
			leftFlex, rightFlex = "1.5", "0.5"
		default:
			leftFlex, rightFlex = "1", "1"
		}
		b.Add(core.BreakpointDesktop, sel+" .dt-line", append([]string{core.CSSDecl("flex", leftFlex)}, lineBase...))
		b.Add(core.BreakpointDesktop, sel+" .dt-line + .dt-inset + .dt-line", nil)
		b.Add(core.BreakpointDesktop, sel+" .dt-line:last-child", append([]string{core.CSSDecl("flex", rightFlex)}, lineBase...))

		insetDecls := []string{"display: inline-flex", "align-items: center"}
		if p.Inset.Spacing != "" {
			insetDecls = append(insetDecls, core.CSSDecl("padding", "0", p.Inset.Spacing))
		}
		insetDecls = append(insetDecls, "white-space: nowrap")
		if p.Inset.FontSize != "" {
			insetDecls = append(insetDecls, core.CSSDecl("font-size", p.Inset.FontSize))
		}
		if p.Inset.FontWeight != "" {
			insetDecls = append(insetDecls, core.CSSDecl("font-weight", p.Inset.FontWeight))
		}
		if p.Inset.Color != "" {
			insetDecls = append(insetDecls, core.CSSDecl("color", p.Inset.Color))
		}
		b.Add(core.BreakpointDesktop, sel+" .dt-inset", insetDecls)
		b.Add(core.BreakpointDesktop, sel+" .dt-inset svg", []string{"width: 1em", "height: 1em"})
	}

	// 总宽度（三端）+ 对齐（非 100%）。
	widthDecl := func(bp string, w string) []string {
		if w == "" {
			return nil
		}
		out := []string{core.CSSDecl("width", w)}
		switch p.Align {
		case "left":
			out = append(out, "margin-left: 0", "margin-right: auto")
		case "right":
			out = append(out, "margin-left: auto", "margin-right: 0")
		default: // center
			out = append(out, "margin-left: auto", "margin-right: auto")
		}
		return out
	}
	if decls := widthDecl(core.BreakpointDesktop, p.Width.Desktop); decls != nil {
		b.Add(core.BreakpointDesktop, sel, decls)
	}
	if decls := widthDecl(core.BreakpointTablet, p.Width.Tablet); decls != nil {
		b.Add(core.BreakpointTablet, sel, decls)
	}
	if decls := widthDecl(core.BreakpointMobile, p.Width.Mobile); decls != nil {
		b.Add(core.BreakpointMobile, sel, decls)
	}
}

// lineColor 线条颜色（主题 Token 或自定义）。
func lineColor(p *Props) string {
	if p.Color == "" {
		return "currentColor"
	}
	return p.Color
}

// StyleOr 线条类型缺省。
func (p *Props) StyleOr(def string) string {
	if p.Style == "" {
		return def
	}
	return p.Style
}

// strWeight 线粗缺省。
func strWeight(p *Props) string {
	if p.Weight == "" {
		return "1px"
	}
	return p.Weight
}

// init 注册分割线组件。
func init() {
	core.Register(Widget)
}
