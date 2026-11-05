package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var (
	SafeValueRe = regexp.MustCompile(`^[A-Za-z0-9#%.,()\-+/:?=&_~ ]*$`)

	cssExternalURLRe = regexp.MustCompile(`(?i)url\s*\(\s*['"]?\s*(?:https?:|//)`)

	cssScriptProtocolRe = regexp.MustCompile(`(?i)\b(?:javascript|vbscript|livescript|mocha|data|blob|file|filesystem)\s*:`)

	cssLegacyExecRe = regexp.MustCompile(`(?i)(?:expression\s*\(|-moz-binding|\bbehavior\s*:)`)

	CustomClassRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

	CustomIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

	LengthValueRe = regexp.MustCompile(`^-?[A-Za-z0-9.%]+$`)
)

var ShadowPresets = map[string]string{
	"sm": "0 1px 3px rgba(0,0,0,0.12)",
	"md": "0 4px 12px rgba(0,0,0,0.12)",
	"lg": "0 10px 28px rgba(0,0,0,0.16)",
	"xl": "0 20px 48px rgba(0,0,0,0.2)",

	"neon": "0 0 8px rgba(59,130,246,.6), 0 0 24px rgba(59,130,246,.35)",
}

const negMarginLimit = 300

const zIndexLimit = 100

const hueRotateLimit = 360

func IsSafeCSSValue(v string) bool {
	if len(v) > 500 {
		return false
	}
	if !SafeValueRe.MatchString(v) {
		return false
	}
	if cssExternalURLRe.MatchString(v) {
		return false
	}

	probe := stripCSSValueNoise(v)
	if cssScriptProtocolRe.MatchString(probe) {
		return false
	}
	return !cssLegacyExecRe.MatchString(probe)
}

func stripCSSValueNoise(v string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r <= 0x20, r == 0x7f, r == 0x180e:
			return -1
		case r > 0x7f && unicode.IsSpace(r):
			return -1
		}
		return r
	}, v)
}

type Spacing struct {
	Top    string `json:"top,omitempty"`
	Right  string `json:"right,omitempty"`
	Bottom string `json:"bottom,omitempty"`
	Left   string `json:"left,omitempty"`
}

func (s Spacing) IsEmpty() bool {
	return s.Top == "" && s.Right == "" && s.Bottom == "" && s.Left == ""
}

func (s Spacing) CSS() string {
	if s.IsEmpty() {
		return ""
	}
	return strings.Join([]string{s.Top, s.Right, s.Bottom, s.Left}, " ")
}

type ResponsiveSpacing struct {
	Desktop Spacing `json:"desktop,omitempty"`
	Tablet  Spacing `json:"tablet,omitempty"`
	Mobile  Spacing `json:"mobile,omitempty"`
}

func validateSpacing(name string, s Spacing, allowNegative bool) (err error) {
	for side, v := range map[string]string{"上": s.Top, "右": s.Right, "下": s.Bottom, "左": s.Left} {
		if v == "" {
			continue
		}
		if !LengthValueRe.MatchString(v) || len(v) > 20 {
			return fmt.Errorf("无效的%s%s: %q", name, side, v)
		}
		if !allowNegative && strings.HasPrefix(v, "-") {
			return fmt.Errorf("%s%s不允许负值: %q", name, side, v)
		}
		if strings.HasPrefix(v, "-") {

			num := strings.TrimPrefix(v, "-")
			i := 0
			for i < len(num) && ((num[i] >= '0' && num[i] <= '9') || num[i] == '.') {
				i++
			}
			if i > 0 {
				if f, e := strconv.ParseFloat(num[:i], 64); e == nil && f > negMarginLimit {
					return fmt.Errorf("%s%s负值超出下限 -%dpx: %q", name, side, negMarginLimit, v)
				}
			}
		}
	}
	return nil
}

var cssDeclsUnsafeRe = regexp.MustCompile("[{}<>\"'\x60]")

func IsSafeCSSDecls(v string) bool {
	if len(v) > 500 {
		return false
	}
	if cssDeclsUnsafeRe.MatchString(v) {
		return false
	}
	low := strings.ToLower(v)
	for _, bad := range []string{"expression", "javascript:", "url(", "@import", "</"} {
		if strings.Contains(low, bad) {
			return false
		}
	}
	return true
}

func parseResponsiveDecls(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, ":") {
			continue
		}
		if !IsSafeCSSValue(part) {
			continue
		}
		out = append(out, part)
	}
	return out
}

const (
	WidthAuto  = "auto"
	WidthFull  = "full"
	WidthFixed = "fixed"
)

var alignSelfMap = map[string]string{
	"start": "flex-start", "center": "center", "end": "flex-end",
	"stretch": "stretch", "baseline": "baseline", "auto": "auto",
}

type AdvancedProps struct {
	Margin ResponsiveSpacing `json:"margin,omitempty" ct:"spacing,sec=layout,label=外距"`

	Padding ResponsiveSpacing `json:"padding,omitempty" ct:"spacing,sec=layout,label=内距"`

	WidthMode string `json:"widthMode,omitempty" ct:"select,auto=自适应,full=铺满父容器,fixed=固定宽度,sec=layout,label=宽度模式"`

	WidthValue string `json:"widthValue,omitempty" ct:"dimension,maxlen=20,sec=layout,label=固定宽度"`

	AlignSelf string `json:"alignSelf,omitempty" ct:"select,auto=默认,start=起始,center=居中,end=末端,stretch=拉伸,baseline=基线,sec=layout,label=自对齐"`

	Border BorderProps `json:"border,omitempty" ct:"group"`

	Radius RadiusProps `json:"radius,omitempty" ct:"group"`

	Shadow string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,neon=霓虹,sec=border,label=阴影"`

	Surface string `json:"surface,omitempty" ct:"select,=标准,glass=玻璃拟态,liquid=液态玻璃,neumorph=新拟态,sec=border,label=表面质感"`

	TextGradient string `json:"textGradient,omitempty" ct:"safe,maxlen=300,sec=advanced,label=渐变文字"`

	Opacity int `json:"opacity,omitempty" ct:"int,min=0,max=100,sec=layout,label=不透明度(%)"`

	HueRotate int `json:"hueRotate,omitempty" ct:"int,min=-360,max=360,sec=style,label=色相偏移(deg)"`

	HideOn HideOn `json:"hideOn,omitempty" ct:"group"`

	ZIndex int `json:"zIndex,omitempty" ct:"int,min=-100,max=100,sec=layout,label=Z-index"`

	Interaction InteractionProps `json:"interaction,omitempty"`

	TabletCSS string `json:"tabletCss,omitempty" ct:"cssdecls,maxlen=500,sec=responsive,label=平板端样式覆盖"`

	MobileCSS string `json:"mobileCss,omitempty" ct:"cssdecls,maxlen=500,sec=responsive,label=手机端样式覆盖"`

	CustomClasses []string `json:"customClasses,omitempty" ct:"classes,sec=advanced,label=CSS 类"`

	CustomID string `json:"customId,omitempty" ct:"safe,maxlen=64,sec=advanced,label=CSS ID"`
}

type BorderProps struct {
	Width string `json:"width,omitempty" ct:"dimension,maxlen=20,sec=border,label=边框宽度"`
	Style string `json:"style,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,sec=border,label=边框样式"`
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=border,label=边框颜色"`

	Gradient string `json:"gradient,omitempty" ct:"safe,maxlen=300,sec=border,label=渐变边框"`

	Flow bool `json:"flow,omitempty" ct:"bool,sec=border,label=边框流动"`
}

func (b BorderProps) IsSet() bool {
	return b.Width != "" || b.Style != "" || b.Color != ""
}

type RadiusProps struct {
	TopLeft     string `json:"topLeft,omitempty" ct:"dimension,maxlen=20,sec=border,label=左上圆角"`
	TopRight    string `json:"topRight,omitempty" ct:"dimension,maxlen=20,sec=border,label=右上圆角"`
	BottomRight string `json:"bottomRight,omitempty" ct:"dimension,maxlen=20,sec=border,label=右下圆角"`
	BottomLeft  string `json:"bottomLeft,omitempty" ct:"dimension,maxlen=20,sec=border,label=左下圆角"`
}

func (r RadiusProps) IsEmpty() bool {
	return r.TopLeft == "" && r.TopRight == "" && r.BottomRight == "" && r.BottomLeft == ""
}

func (r RadiusProps) CSS() string {
	if r.IsEmpty() {
		return ""
	}
	return strings.Join([]string{r.TopLeft, r.TopRight, r.BottomRight, r.BottomLeft}, " ")
}

type HideOn struct {
	Desktop bool `json:"desktop,omitempty" ct:"bool,sec=layout,label=桌面隐藏"`
	Tablet  bool `json:"tablet,omitempty" ct:"bool,sec=layout,label=平板隐藏"`
	Mobile  bool `json:"mobile,omitempty" ct:"bool,sec=layout,label=手机隐藏"`
}

func (h HideOn) IsEmpty() bool {
	return !h.Desktop && !h.Tablet && !h.Mobile
}

var allowedBorderStyle = map[string]bool{
	"solid": true, "dashed": true, "dotted": true, "double": true,
}

func ValidateAdvanced(a *AdvancedProps, nodeID string, ids map[string]bool) (err error) {

	if err = ValidateInteraction(a.Interaction); err != nil {
		return fmt.Errorf("节点 %s: %w", nodeID, err)
	}
	for bp, rs := range map[string]Spacing{
		"desktop": a.Margin.Desktop, "tablet": a.Margin.Tablet, "mobile": a.Margin.Mobile,
	} {
		if err = validateSpacing(fmt.Sprintf("%s 端外边距", bp), rs, true); err != nil {
			return fmt.Errorf("节点 %s: %w", nodeID, err)
		}
	}
	for bp, rs := range map[string]Spacing{
		"desktop": a.Padding.Desktop, "tablet": a.Padding.Tablet, "mobile": a.Padding.Mobile,
	} {
		if err = validateSpacing(fmt.Sprintf("%s 端内边距", bp), rs, false); err != nil {
			return fmt.Errorf("节点 %s: %w", nodeID, err)
		}
	}

	switch a.WidthMode {
	case "", WidthAuto, WidthFull:
	case WidthFixed:
		if !IsSafeCSSValue(a.WidthValue) || a.WidthValue == "" {
			return fmt.Errorf("节点 %s: fixed 宽度模式必须提供有效宽度值: %q", nodeID, a.WidthValue)
		}
	default:
		return fmt.Errorf("节点 %s: 无效的宽度模式: %q", nodeID, a.WidthMode)
	}

	if a.AlignSelf != "" {
		if _, ok := alignSelfMap[a.AlignSelf]; !ok {
			return fmt.Errorf("节点 %s: 无效的自身对齐: %q", nodeID, a.AlignSelf)
		}
	}

	if a.Border.IsSet() {
		if a.Border.Width == "" || a.Border.Style == "" || a.Border.Color == "" {
			return fmt.Errorf("节点 %s: 边框需同时提供粗细、线型与颜色", nodeID)
		}
		if !IsSafeCSSValue(a.Border.Width) || !IsSafeCSSValue(a.Border.Color) {
			return fmt.Errorf("节点 %s: 无效的边框值", nodeID)
		}
		if !allowedBorderStyle[a.Border.Style] {
			return fmt.Errorf("节点 %s: 无效的边框线型: %q", nodeID, a.Border.Style)
		}
	}

	for name, v := range map[string]string{
		"左上圆角": a.Radius.TopLeft, "右上圆角": a.Radius.TopRight,
		"右下圆角": a.Radius.BottomRight, "左下圆角": a.Radius.BottomLeft,
	} {
		if v != "" && (!IsSafeCSSValue(v) || len(v) > 20) {
			return fmt.Errorf("节点 %s: 无效的%s: %q", nodeID, name, v)
		}
	}

	if a.Shadow != "" {
		if _, ok := ShadowPresets[a.Shadow]; !ok {
			return fmt.Errorf("节点 %s: 无效的阴影预设: %q", nodeID, a.Shadow)
		}
	}

	if !allowedSurface[a.Surface] {
		return fmt.Errorf("节点 %s: 无效的表面质感: %q（glass/liquid）", nodeID, a.Surface)
	}

	if a.Border.Gradient != "" {
		if !IsSafeCSSValue(a.Border.Gradient) {
			return fmt.Errorf("节点 %s: 无效的渐变边框: %q", nodeID, a.Border.Gradient)
		}
		if a.Border.Flow && !strings.Contains(a.Border.Gradient, "conic-gradient(") {
			return fmt.Errorf("节点 %s: 边框流动需 conic-gradient 渐变", nodeID)
		}
	}

	if a.TextGradient != "" && !IsSafeCSSValue(a.TextGradient) {
		return fmt.Errorf("节点 %s: 无效的渐变文字: %q", nodeID, a.TextGradient)
	}
	if a.Opacity < 0 || a.Opacity > 100 {
		return fmt.Errorf("节点 %s: 不透明度必须在 0~100 之间: %d", nodeID, a.Opacity)
	}
	if a.ZIndex < -zIndexLimit || a.ZIndex > zIndexLimit {
		return fmt.Errorf("节点 %s: z-index 必须在 [-%d, %d] 之间: %d", nodeID, zIndexLimit, zIndexLimit, a.ZIndex)
	}
	if a.HueRotate < -hueRotateLimit || a.HueRotate > hueRotateLimit {
		return fmt.Errorf("节点 %s: 色相偏移必须在 [-%d, %d] 度之间: %d", nodeID, hueRotateLimit, hueRotateLimit, a.HueRotate)
	}

	for _, cls := range a.CustomClasses {
		if !CustomClassRe.MatchString(cls) {
			return fmt.Errorf("节点 %s: 无效的自定义 class: %q", nodeID, cls)
		}
		if strings.HasPrefix(cls, "sky-") {
			return fmt.Errorf("节点 %s: 自定义 class 禁止使用 sky- 保留前缀: %q", nodeID, cls)
		}
	}
	if a.CustomID != "" {
		if !CustomIDRe.MatchString(a.CustomID) {
			return fmt.Errorf("节点 %s: 无效的自定义 ID: %q", nodeID, a.CustomID)
		}
		if ids[a.CustomID] {
			return fmt.Errorf("节点 %s: 自定义 ID 重复: %q", nodeID, a.CustomID)
		}
		ids[a.CustomID] = true
	}
	return nil
}

func CompileAdvanced(nodeID string, a *AdvancedProps, b *CSSBuckets) (extraClasses []string, customID string) {
	sel := "." + NodeClass(nodeID)

	var desktop, tablet, mobile []string

	appendSpacing := func(prop string, s Spacing) {
		set := 0
		for _, v := range []string{s.Top, s.Right, s.Bottom, s.Left} {
			if v != "" {
				set++
			}
		}
		switch set {
		case 0:
		case 4:
			desktop = append(desktop, prop+": "+s.CSS())
		default:
			if s.Top != "" {
				desktop = append(desktop, prop+"-top: "+s.Top)
			}
			if s.Right != "" {
				desktop = append(desktop, prop+"-right: "+s.Right)
			}
			if s.Bottom != "" {
				desktop = append(desktop, prop+"-bottom: "+s.Bottom)
			}
			if s.Left != "" {
				desktop = append(desktop, prop+"-left: "+s.Left)
			}
		}
	}
	appendSpacing("margin", a.Margin.Desktop)
	if v := a.Margin.Tablet; !v.IsEmpty() {
		tablet = append(tablet, spacingDecls("margin", v)...)
	}
	if v := a.Margin.Mobile; !v.IsEmpty() {
		mobile = append(mobile, spacingDecls("margin", v)...)
	}
	appendSpacing("padding", a.Padding.Desktop)
	if v := a.Padding.Tablet; !v.IsEmpty() {
		tablet = append(tablet, spacingDecls("padding", v)...)
	}
	if v := a.Padding.Mobile; !v.IsEmpty() {
		mobile = append(mobile, spacingDecls("padding", v)...)
	}

	switch a.WidthMode {
	case WidthFull:
		desktop = append(desktop, "width: 100%")
	case WidthFixed:
		desktop = append(desktop, "width: "+a.WidthValue)
	}
	if a.AlignSelf != "" && a.AlignSelf != "auto" {
		desktop = append(desktop, "align-self: "+alignSelfMap[a.AlignSelf])
	}

	if a.Border.IsSet() {
		desktop = append(desktop, "border: "+a.Border.Width+" "+a.Border.Style+" "+a.Border.Color)
	}
	if v := a.Radius.CSS(); v != "" {
		desktop = append(desktop, "border-radius: "+v)
	}

	if a.Border.Gradient != "" {
		bw := a.Border.Width
		if bw == "" {
			bw = "2px"
		}
		desktop = append(desktop, "border: "+bw+" solid transparent", "border-image: "+a.Border.Gradient+" 1")
		if a.Border.Flow {
			desktop = append(desktop, "animation: sky-border-flow 3s linear infinite")
			b.NeedKeyframes("sky-border-flow")
			b.AddKeyframes("sky-border-flow-angle", BorderFlowAngleProperty)
		}
	}

	if v, ok := ShadowPresets[a.Shadow]; a.Shadow != "" && ok {
		desktop = append(desktop, "box-shadow: "+v)
	}

	CompileSurface(sel, a.Surface, b)

	if a.TextGradient != "" {
		desktop = append(desktop, TextGradientDecls(a.TextGradient)...)
	}
	if a.Opacity > 0 && a.Opacity < 100 {
		desktop = append(desktop, fmt.Sprintf("opacity: %s", strconv.FormatFloat(float64(a.Opacity)/100, 'f', 2, 64)))
	}

	if a.HueRotate != 0 {
		desktop = append(desktop, fmt.Sprintf("filter: hue-rotate(%ddeg)", a.HueRotate))
	}
	if a.ZIndex != 0 {
		desktop = append(desktop, fmt.Sprintf("z-index: %d", a.ZIndex))
	} else if a.Interaction.Sticky {

		desktop = append(desktop, "z-index: 10")
	}

	tablet = append(tablet, parseResponsiveDecls(a.TabletCSS)...)
	mobile = append(mobile, parseResponsiveDecls(a.MobileCSS)...)

	b.Add(BreakpointDesktop, sel, desktop)
	b.Add(BreakpointTablet, sel, tablet)
	b.Add(BreakpointMobile, sel, mobile)

	if a.HideOn.Desktop {
		b.Add(BreakpointDesktop, sel, []string{"display: none"})
	}
	if a.HideOn.Tablet {
		b.Add(BreakpointTablet, sel, []string{"display: none"})
	}
	if a.HideOn.Mobile {
		b.Add(BreakpointMobile, sel, []string{"display: none"})
	}

	CompileInteraction(sel, a.Interaction, b)

	return a.CustomClasses, a.CustomID
}

func spacingDecls(prop string, s Spacing) (decls []string) {
	if s.Top != "" {
		decls = append(decls, prop+"-top: "+s.Top)
	}
	if s.Right != "" {
		decls = append(decls, prop+"-right: "+s.Right)
	}
	if s.Bottom != "" {
		decls = append(decls, prop+"-bottom: "+s.Bottom)
	}
	if s.Left != "" {
		decls = append(decls, prop+"-left: "+s.Left)
	}
	return decls
}
