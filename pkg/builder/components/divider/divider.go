package divider

import (
	_ "embed"
	"fmt"

	"go_wp/pkg/builder/core"
)

const Type = "core.divider"

const (
	LineSolid  = "solid"
	LineDashed = "dashed"
	LineDotted = "dotted"
	LineDouble = "double"
)

const (
	InsetNone = "none"
	InsetText = "text"
	InsetIcon = "icon"
)

const (
	PosCenter = "center"
	PosLeft   = "left"
	PosRight  = "right"
)

var insetIconNames = map[string]string{
	"star":    "star",
	"diamond": "diamond",
	"dot":     "circle",
}

type Width struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

type Inset struct {
	Kind string `json:"kind,omitempty" ct:"select,none=纯线,text=嵌入文本,icon=嵌入图标,default=none,sec=content,label=样式"`

	Text string `json:"text,omitempty" ct:"text,maxlen=50,sec=content"`

	FontSize   string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style"`
	FontWeight string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,sec=style,label=字重"`
	Color      string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`

	IconName string `json:"iconName,omitempty" ct:"select,star=星形,diamond=菱形,dot=圆点,sec=content,label=图标样式"`

	Position string `json:"position,omitempty" ct:"select,center=居中,left=靠左,right=靠右,default=center,sec=content,label=位置"`

	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style"`
}

type Props struct {
	Style string `json:"style,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,default=solid,sec=style,label=线型"`

	Weight string `json:"weight,omitempty" ct:"dimension,maxlen=20,sec=style"`

	Width Width `json:"width,omitempty"`

	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`

	Inset Inset `json:"inset,omitempty"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "分隔线",
		Hint:            "内容分隔",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"style":  "solid",
			"weight": "1px",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"text"},
	},
}

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
		if _, ok := insetIconNames[p.Inset.IconName]; !ok {
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

//go:embed divider.css
var dividerCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	hasInset := !(p.Inset.Kind == InsetNone || p.Inset.Kind == "")

	leftFlex, rightFlex := "1", "1"
	switch p.Inset.Position {
	case PosLeft:
		leftFlex, rightFlex = "0.5", "1.5"
	case PosRight:
		leftFlex, rightFlex = "1.5", "0.5"
	}

	vars := map[string]string{
		"no_inset":          core.BoolVar(!hasInset),
		"has_inset":         core.BoolVar(hasInset),
		"line_decl":         lineDecl(p),
		"left_flex":         leftFlex,
		"right_flex":        rightFlex,
		"inset_spacing":     p.Inset.Spacing,
		"inset_font_size":   p.Inset.FontSize,
		"inset_font_weight": p.Inset.FontWeight,
		"inset_color":       p.Inset.Color,
	}

	setWidth := func(suffix, w string) {
		vars["width_"+suffix] = w
		if w == "" {
			vars["ml_"+suffix], vars["mr_"+suffix] = "", ""
			return
		}
		switch p.Align {
		case "left":
			vars["ml_"+suffix], vars["mr_"+suffix] = "0", "auto"
		case "right":
			vars["ml_"+suffix], vars["mr_"+suffix] = "auto", "0"
		default:
			vars["ml_"+suffix], vars["mr_"+suffix] = "auto", "auto"
		}
	}
	setWidth("desktop", p.Width.Desktop)
	setWidth("tablet", p.Width.Tablet)
	setWidth("mobile", p.Width.Mobile)

	if err := core.ApplyComponentCSSTmpl(b, sel, dividerCSS, vars); err != nil {

		panic(fmt.Sprintf("divider 组件样式解析失败: %v", err))
	}
}

func lineDecl(p *Props) string {
	style := p.Style
	if style == "" {
		style = LineSolid
	}
	weight := p.Weight
	if weight == "" {
		weight = "1px"
	}
	if style == LineDouble && weight == "1px" {
		weight = "3px"
	}
	return core.CSSDecl("border-top", weight, style, lineColor(p))
}

func lineColor(p *Props) string {
	if p.Color == "" {
		return "currentColor"
	}
	return p.Color
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("divider", dividerTemplate)
}

//go:embed divider.jet
var dividerTemplate string
