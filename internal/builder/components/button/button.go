package button

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.button"

const (
	ActionInternal = "internal"
	ActionExternal = "external"
	ActionAnchor   = "anchor"
	ActionNative   = "native"
	ActionModal    = "modal"
	ActionLink     = "link"
)

const (
	SizeXS = "xs"
	SizeSM = "sm"
	SizeMD = "md"
	SizeLG = "lg"
	SizeXL = "xl"
)

const (
	VariantSolid   = "solid"
	VariantOutline = "outline"
	VariantGhost   = "ghost"
)

var sizePresets = map[string][2]string{
	SizeXS: {"4px 10px", "0.75rem"},
	SizeSM: {"6px 14px", "0.875rem"},
	SizeMD: {"10px 20px", "1rem"},
	SizeLG: {"12px 26px", "1.125rem"},
	SizeXL: {"16px 34px", "1.25rem"},
}

var iconNames = map[string]string{
	"arrow-right":   "arrow-right",
	"arrow-left":    "arrow-left",
	"arrow-up":      "arrow-up",
	"arrow-down":    "arrow-down",
	"check":         "check",
	"chevron-right": "chevron-right",
	"phone":         "phone",
	"mail":          "mail",
}

type Icon struct {
	Source string `json:"source,omitempty" ct:"select,builtin=内置图标,media=媒体库图片,sec=content,label=图标来源"`

	Name string `json:"name,omitempty" ct:"select,arrow-right=右箭头,arrow-left=左箭头,arrow-up=上箭头,arrow-down=下箭头,check=对勾,chevron-right=右尖括号,phone=电话,mail=邮件,sec=content,label=图标样式"`

	URL string `json:"url,omitempty" ct:"media,sec=content,label=图标图片"`

	Position string `json:"position,omitempty" ct:"select,prefix=图标在前,suffix=图标在后,top=图标在上,bottom=图标在下,sec=content,label=图标位置"`

	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style"`

	HoverShift string `json:"hoverShift,omitempty" ct:"dimension,maxlen=20,sec=style"`

	Size string `json:"size,omitempty" ct:"dimension,maxlen=20,sec=style"`
}

type State struct {
	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style"`
	Color      string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`
	Border     string `json:"border,omitempty" ct:"color,maxlen=200,sec=style"`
	Shadow     string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=style,label=阴影级别"`
}

type Block struct {
	Desktop bool `json:"desktop,omitempty"`
	Tablet  bool `json:"tablet,omitempty"`
	Mobile  bool `json:"mobile,omitempty"`
}

type Binding struct {
	Field string `json:"field,omitempty"`
}

type Props struct {
	Text string `json:"text,omitempty" ct:"string,maxlen=200,sec=content"`

	Binding *Binding `json:"binding,omitempty"`

	Action string `json:"action,omitempty" ct:"select,internal=站内链接,external=外部链接,anchor=页内锚点,native=电话/邮件,modal=弹窗,link=自定义链接,default=external,sec=content,label=点击动作"`

	Value string `json:"value,omitempty" ct:"string,maxlen=500,sec=content"`

	Target string `json:"target,omitempty" ct:"select,self=当前窗口,blank=新窗口,sec=content,label=打开方式"`

	Rel string `json:"rel,omitempty" ct:"select,none=默认,nofollow=加 nofollow,sponsored=赞助链接,sec=content,label=链接关系"`

	FontSize      string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style"`
	FontWeight    string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,800=特粗,sec=style,label=字重"`
	LetterSpacing string `json:"letterSpacing,omitempty" ct:"dimension,maxlen=20,sec=style"`
	Transform     string `json:"transform,omitempty" ct:"select,none=无,uppercase=全大写,lowercase=全小写,capitalize=首字母大写,sec=style,label=大小写转换"`

	Icon *Icon `json:"icon,omitempty"`

	Size string `json:"size,omitempty" ct:"select,xs=特小,sm=小,md=中,lg=大,xl=特大,default=md,sec=style,label=按钮尺寸"`

	FontFamily string `json:"fontFamily,omitempty" ct:"safe,maxlen=200,sec=style,label=字体族"`

	FullWidth bool `json:"fullWidth,omitempty" ct:"bool,sec=style,label=全宽"`

	Bg string `json:"bg,omitempty" ct:"color,maxlen=200,sec=background,label=背景色"`

	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文字颜色"`

	BorderWidth string `json:"borderWidth,omitempty" ct:"dimension,maxlen=20,sec=border,label=边框宽度"`

	BorderStyle string `json:"borderStyle,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,sec=border,label=边框样式"`

	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=200,sec=border,label=边框颜色"`

	RadiusTL string `json:"radiusTL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左上圆角"`
	RadiusTR string `json:"radiusTR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右上圆角"`
	RadiusBR string `json:"radiusBR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右下圆角"`
	RadiusBL string `json:"radiusBL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左下圆角"`

	Shadow string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=border,label=阴影"`

	HoverBorderColor string `json:"hoverBorderColor,omitempty" ct:"color,maxlen=200,sec=border,label=悬停边框色"`

	HoverShadow string `json:"hoverShadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=border,label=悬停阴影"`

	HoverBg string `json:"hoverBg,omitempty" ct:"color,maxlen=200,sec=background,label=悬停背景色"`

	HoverColor string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停文字色"`

	LineHeight string `json:"lineHeight,omitempty" ct:"dimension,maxlen=20,sec=style,label=行高"`
	Block      Block  `json:"block,omitempty"`
	Variant    string `json:"variant,omitempty" ct:"select,solid,outline,ghost,default=solid,sec=style"`

	Normal State `json:"normal,omitempty"`
	Hover  State `json:"hover,omitempty"`

	HoverLift string `json:"hoverLift,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停上浮"`

	Rotate string `json:"rotate,omitempty" ct:"dimension,maxlen=20,sec=transform,label=旋转"`

	TranslateX string `json:"translateX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=水平偏移"`
	TranslateY string `json:"translateY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=垂直偏移"`

	Scale string `json:"scale,omitempty" ct:"safe,maxlen=20,sec=transform,label=缩放"`

	SkewX string `json:"skewX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=倾斜 X"`
	SkewY string `json:"skewY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=倾斜 Y"`

	FlipX bool `json:"flipX,omitempty" ct:"bool,sec=transform,label=水平翻转"`
	FlipY bool `json:"flipY,omitempty" ct:"bool,sec=transform,label=垂直翻转"`

	HoverRotate     string `json:"hoverRotate,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停旋转"`
	HoverTranslateX string `json:"hoverTranslateX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停水平偏移"`
	HoverTranslateY string `json:"hoverTranslateY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停垂直偏移"`
	HoverScale      string `json:"hoverScale,omitempty" ct:"safe,maxlen=20,sec=transform,label=悬停缩放"`
	HoverSkewX      string `json:"hoverSkewX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停倾斜 X"`
	HoverSkewY      string `json:"hoverSkewY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停倾斜 Y"`

	TransitionDuration string `json:"transitionDuration,omitempty" ct:"dimension,maxlen=20,sec=motion,label=过渡时长"`

	TransitionEasing string `json:"transitionEasing,omitempty" ct:"select,linear=线性,ease=缓入缓出,ease-in=缓入,ease-out=缓出,ease-in-out=先缓入再缓出,sec=motion,label=缓动曲线"`

	TransitionDelay string `json:"transitionDelay,omitempty" ct:"dimension,maxlen=20,sec=motion,label=过渡延迟"`

	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图标加载"`

	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "按钮",
		Hint:            "行动按钮",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"text":   "了解更多",
			"action": "internal",
			"value":  "/",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"text"},
	},
}

var (
	internalPathRe = regexp.MustCompile(`^/[A-Za-z0-9/-]{0,200}$`)
	anchorIDRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	natualRe       = regexp.MustCompile(`^(tel:|mailto:)[^ \x00-\x20]{3,200}$`)
	fieldPathRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
)

func validateExtra(p *Props, nodeID string) (err error) {
	if p.Text == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("必须提供按钮文本或动态链接绑定")
	}
	if p.Binding != nil && p.Binding.Field != "" && !fieldPathRe.MatchString(p.Binding.Field) {
		return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
	}
	if p.Binding != nil && p.Binding.Field != "" && p.Action != ActionLink {
		return fmt.Errorf("动态链接绑定仅支持 link 动作")
	}
	switch p.Action {
	case ActionInternal:
		if p.Value == "" || !internalPathRe.MatchString(p.Value) {
			return fmt.Errorf("内部页面必须提供站内路径（如 /products/xxx）")
		}
	case ActionExternal:
		if p.Value == "" || !isSafeURL(p.Value) {
			return fmt.Errorf("外部链接非法: %q", p.Value)
		}
	case ActionAnchor:
		if !anchorIDRe.MatchString(p.Value) {
			return fmt.Errorf("锚点必须提供合法元素 ID: %q", p.Value)
		}
	case ActionNative:
		if !natualRe.MatchString(p.Value) {
			return fmt.Errorf("原生动作仅支持 tel:/mailto: 协议: %q", p.Value)
		}
	case ActionModal:
		if !anchorIDRe.MatchString(p.Value) {
			return fmt.Errorf("弹窗绑定必须提供目标组件 ID: %q", p.Value)
		}
	case ActionLink:

	default:
		return fmt.Errorf("无效的动作类型: %q", p.Action)
	}
	if p.Icon != nil {
		if p.Icon.Source == "builtin" {
			if _, ok := iconNames[p.Icon.Name]; !ok {
				return fmt.Errorf("无效的内置图标: %q（白名单见规范）", p.Icon.Name)
			}
		} else if p.Icon.Source == "media" {
			if p.Icon.URL == "" || strings.ContainsAny(p.Icon.URL, " \t\"'<>`;") {
				return fmt.Errorf("无效的图标地址: %q", p.Icon.URL)
			}
		} else {
			return fmt.Errorf("无效的图标源: %q", p.Icon.Source)
		}
		if p.Icon.Position != "" && p.Icon.Position != "prefix" && p.Icon.Position != "suffix" {
			return fmt.Errorf("图标位置仅支持 prefix/suffix: %q", p.Icon.Position)
		}
	}
	return nil
}

//go:embed button.css
var buttonCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	sizePadding, sizeFontSize := "10px 20px", "1rem"
	if preset, ok := sizePresets[p.Size]; ok {
		sizePadding, sizeFontSize = preset[0], preset[1]
	}

	textTransform := ""
	if p.Transform != "" && p.Transform != "none" {
		textTransform = p.Transform
	}

	radius := "var(--sky-btn-radius, 8px)"
	if p.RadiusTL != "" || p.RadiusTR != "" || p.RadiusBR != "" || p.RadiusBL != "" {
		corner := func(v string) string {
			if v == "" {
				return "0"
			}
			return v
		}
		radius = strings.Join([]string{
			corner(p.RadiusTL), corner(p.RadiusTR), corner(p.RadiusBR), corner(p.RadiusBL),
		}, " ")
	}

	iconSpacing, iconShift := "", ""
	iconStacked, iconShiftOn := false, false
	if p.Icon != nil {
		iconSpacing = p.Icon.Spacing
		iconStacked = p.Icon.Position == "top" || p.Icon.Position == "bottom"
		iconShift = p.Icon.HoverShift
		iconShiftOn = p.Icon.HoverShift != ""
	}

	bg := p.Bg
	if bg == "" {
		bg = p.Normal.Background
	}
	textColor := p.TextColor
	if textColor == "" {
		textColor = p.Normal.Color
	}
	borderColor := p.BorderColor
	if borderColor == "" {
		borderColor = p.Normal.Border
	}
	shadowLevel := p.Shadow
	if shadowLevel == "" {
		shadowLevel = p.Normal.Shadow
	}

	variant := p.Variant
	switch variant {
	case VariantGhost, VariantOutline:
	default:
		variant = VariantSolid
	}

	transparentBg := "transparent"
	if bg != "" {
		transparentBg = bg
	}
	solidBg := "var(--sky-btn-bg, var(--sky-c-primary, #2563eb))"
	if bg != "" {
		solidBg = bg
	}
	solidColor := "var(--sky-btn-color, #fff)"
	if textColor != "" {
		solidColor = textColor
	}
	solidBorder := borderValue(p, borderColor,
		"var(--sky-btn-border-width, 0) var(--sky-btn-border-style, solid) var(--sky-btn-border-color, transparent)")

	shadow := "var(--sky-btn-shadow, none)"
	if v, ok := core.ShadowPresets[shadowLevel]; ok {
		shadow = v
	}

	hoverBg := p.HoverBg
	if hoverBg == "" {
		hoverBg = p.Hover.Background
	}
	switch variant {
	case VariantOutline:
		if hoverBg == "" {
			hoverBg = textColor
		}
	default:
		if hoverBg == "" {
			hoverBg = bg
		}
	}
	hoverColor := p.HoverColor
	if hoverColor == "" {
		hoverColor = p.Hover.Color
	}
	hoverBorder := p.HoverBorderColor
	if hoverBorder == "" {
		hoverBorder = p.Hover.Border
	}
	hoverShadow := p.HoverShadow
	if hoverShadow == "" {
		hoverShadow = p.Hover.Shadow
	}
	hoverShadowValue := ""
	if v, ok := core.ShadowPresets[hoverShadow]; ok {
		hoverShadowValue = v
	}
	hoverTransform := transformValue(p, true)

	hoverState := hoverBg != "" || hoverColor != "" || hoverBorder != "" ||
		hoverShadowValue != "" || hoverTransform != ""

	vars := map[string]string{
		"sizePadding":    sizePadding,
		"sizeFontSize":   sizeFontSize,
		"fontSize":       p.FontSize,
		"fontWeight":     p.FontWeight,
		"letterSpacing":  p.LetterSpacing,
		"textTransform":  textTransform,
		"radius":         radius,
		"fontFamily":     p.FontFamily,
		"lineHeight":     p.LineHeight,
		"fullWidth":      core.BoolVar(p.FullWidth),
		"iconSpacing":    iconSpacing,
		"iconStacked":    core.BoolVar(iconStacked),
		"variantGhost":   core.BoolVar(variant == VariantGhost),
		"ghostBg":        transparentBg,
		"ghostCompact":   core.BoolVar(p.Text == ""),
		"variantOutline": core.BoolVar(variant == VariantOutline),
		"outlineBg":      transparentBg,
		"outlineBorder":  borderValue(p, borderColor, "1px solid currentColor"),
		"outlineColor":   textColor,
		"variantSolid":   core.BoolVar(variant == VariantSolid),
		"solidBg":        solidBg,
		"solidColor":     solidColor,
		"solidBorder":    solidBorder,
		"shadow":         shadow,
		"transform":      transformValue(p, false),

		"hoverState":       core.BoolVar(hoverState),
		"transition":       transitionValue(p),
		"hoverStateBg":     hoverBg,
		"hoverStateColor":  hoverColor,
		"hoverStateBorder": hoverBorder,
		"hoverStateShadow": hoverShadowValue,
		"hoverTransform":   hoverTransform,
		"iconShiftOn":      core.BoolVar(iconShiftOn),
		"iconShift":        iconShift,
		"blockDesktop":     core.BoolVar(p.Block.Desktop),
		"blockTablet":      core.BoolVar(p.Block.Tablet),
		"blockMobile":      core.BoolVar(p.Block.Mobile),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, buttonCSS, vars); err != nil {

		panic(fmt.Sprintf("button 组件样式解析失败: %v", err))
	}
}

func borderValue(p *Props, borderColor, fallback string) string {
	if p.BorderWidth != "" {
		style := p.BorderStyle
		if style == "" {
			style = "solid"
		}
		color := borderColor
		if color == "" {
			color = "currentColor"
		}
		return strings.Join([]string{p.BorderWidth, style, color}, " ")
	}
	if borderColor != "" {
		return "1px solid " + borderColor
	}
	return fallback
}

func transformValue(p *Props, hover bool) string {
	rotate, tx, ty, scale, skewX, skewY := p.Rotate, p.TranslateX, p.TranslateY, p.Scale, p.SkewX, p.SkewY
	flipX, flipY := p.FlipX, p.FlipY
	if hover {
		if p.HoverRotate == "" && p.HoverTranslateX == "" && p.HoverTranslateY == "" &&
			p.HoverScale == "" && p.HoverSkewX == "" && p.HoverSkewY == "" && p.HoverLift == "" {
			return ""
		}
		if p.HoverRotate != "" {
			rotate = p.HoverRotate
		}
		if p.HoverTranslateX != "" {
			tx = p.HoverTranslateX
		}
		if p.HoverTranslateY != "" {
			ty = p.HoverTranslateY
		} else if p.HoverLift != "" {
			ty = p.HoverLift
		}
		if p.HoverScale != "" {
			scale = p.HoverScale
		}
		if p.HoverSkewX != "" {
			skewX = p.HoverSkewX
		}
		if p.HoverSkewY != "" {
			skewY = p.HoverSkewY
		}
	}
	var parts []string
	if rotate != "" {
		parts = append(parts, "rotate("+rotate+")")
	}
	if tx != "" || ty != "" {
		x, y := tx, ty
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "0"
		}
		parts = append(parts, "translate("+x+", "+y+")")
	}
	if scale != "" {
		parts = append(parts, "scale("+scale+")")
	}
	if skewX != "" || skewY != "" {
		x, y := skewX, skewY
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "0"
		}
		parts = append(parts, "skew("+x+", "+y+")")
	}
	if flipX {
		parts = append(parts, "scaleX(-1)")
	}
	if flipY {
		parts = append(parts, "scaleY(-1)")
	}
	return strings.Join(parts, " ")
}

func transitionValue(p *Props) string {
	d := p.TransitionDuration
	if d == "" {
		d = "0.2s"
	}
	e := p.TransitionEasing
	if e == "" {
		e = "ease"
	}
	parts := []string{"all", d, e}
	if p.TransitionDelay != "" {
		parts = append(parts, p.TransitionDelay)
	}
	return strings.Join(parts, " ")
}

func isSafeURL(s string) bool {
	if len(s) > 500 {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("./:?=&%~#+_@-", r)) {
			return false
		}
	}
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("button", buttonTemplate)
}

//go:embed button.jet
var buttonTemplate string
