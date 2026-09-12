// Package button 实现 core.button 按钮与行动召唤组件（规范《02-C5 按钮与 CTA 组件规范》）。
//
// 设计范式要点：
//   - 单层语义化标签：按动作类型智能编译为 <a>（跳转/原生协议）或 <button>（弹窗/表单触发），
//     杜绝 Elementor 的 wrapper 冗余嵌套；
//   - 统一链接协议：internal（站内路径）/ external（外链+target+rel nofollow/sponsored）/
//     anchor（锚点平滑滚动）/ native（tel://mailto:）/ modal（按钮触发）五种动作，
//     支持 CMS 动态链接绑定；
//   - 文案 + 图标（内置白名单 SVG / 媒体库 SVG 内联）+ 双态外观（normal/hover）为
//     后续卡片/横幅等复合预设复用。
package button

import (
	_ "embed" // button.css 经 //go:embed 打进二进制
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.button"

// 动作类型。
const (
	ActionInternal = "internal" // 站内路径
	ActionExternal = "external" // 外部 URL
	ActionAnchor   = "anchor"   // 锚点滚动
	ActionNative   = "native"   // tel://mailto:
	ActionModal    = "modal"    // 唤起弹窗（<button>）
	ActionLink     = "link"     // 动态 CMS 链接绑定
)

// 尺寸预设。
const (
	SizeXS = "xs"
	SizeSM = "sm"
	SizeMD = "md"
	SizeLG = "lg"
	SizeXL = "xl"
)

// 变体。
const (
	VariantSolid   = "solid"   // 实色填充
	VariantOutline = "outline" // 轮廓描边（悬停填充）
	VariantGhost   = "ghost"   // 幽灵文本
)

// 尺寸预设表：padding + 基准字号。
var sizePresets = map[string][2]string{
	SizeXS: {"4px 10px", "0.75rem"},
	SizeSM: {"6px 14px", "0.875rem"},
	SizeMD: {"10px 20px", "1rem"},
	SizeLG: {"12px 26px", "1.125rem"},
	SizeXL: {"16px 34px", "1.25rem"},
}

// Arrows/等内置图标（24 viewBox，stroke currentColor，白名单）。
var builtinIcons = map[string]string{
	"arrow-right":   `<path d="M14 5l7 7m0 0l-7 7m7-7H3" stroke-linecap="round"/>`,
	"arrow-left":    `<path d="M10 5l-7 7m0 0l7 7m-7-7h21" stroke-linecap="round"/>`,
	"arrow-up":      `<path d="M19 14l-7-7m0 0l-7 7m7-7v21" stroke-linecap="round"/>`,
	"arrow-down":    `<path d="M19 10l-7 7m0 0l-7-7m7 7V3" stroke-linecap="round"/>`,
	"check":         `<path d="M20 6L9 17l-5-5" stroke-linecap="round" stroke-linejoin="round"/>`,
	"chevron-right": `<path d="M9 6l6 6-6 6" stroke-linecap="round" stroke-linejoin="round"/>`,
	"phone":         `<path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6A19.79 19.79 0 0 1 2.08 4.18 2 2 0 0 1 4.06 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z"/>`,
	"mail":          `<path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z" stroke-linecap="round"/><path d="M22 6l-10 7L2 6" stroke-linecap="round"/>`,
}

// Icon 图标配置。
type Icon struct {
	// Source 图标源：builtin（内置白名单）/ media（媒体库/外链 URL）。
	Source string `json:"source,omitempty" ct:"select,builtin=内置图标,media=媒体库图片,sec=content,label=图标来源"`
	// Name builtin 图标名（source=builtin）。
	Name string `json:"name,omitempty" ct:"select,arrow-right=右箭头,arrow-left=左箭头,arrow-up=上箭头,arrow-down=下箭头,check=对勾,chevron-right=右尖括号,phone=电话,mail=邮件,sec=content,label=图标样式"`
	// URL 媒体库/外链图标 URL（source=media，img 直引）。
	URL string `json:"url,omitempty" ct:"media,sec=content,label=图标图片"`
	// Position 位置：prefix/suffix（左右）、top/bottom（上下，按钮改为纵向排列）。
	Position string `json:"position,omitempty" ct:"select,prefix=图标在前,suffix=图标在后,top=图标在上,bottom=图标在下,sec=content,label=图标位置"`
	// Spacing 图标与文案间距。
	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style"`
	// HoverShift 悬停时图标水平位移动画值（如 "4px"）。
	HoverShift string `json:"hoverShift,omitempty" ct:"dimension,maxlen=20,sec=style"`
	// Size 图标尺寸。
	Size string `json:"size,omitempty" ct:"dimension,maxlen=20,sec=style"`
}

// State 正常/悬浮双态外观。
type State struct {
	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style"`
	Color      string `json:"color,omitempty" ct:"color,maxlen=200,sec=style"`
	Border     string `json:"border,omitempty" ct:"color,maxlen=200,sec=style"` // 边框颜色
	Shadow     string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=style,label=阴影级别"`
}

// Block 三端块级铺满。
type Block struct {
	Desktop bool `json:"desktop,omitempty"`
	Tablet  bool `json:"tablet,omitempty"`
	Mobile  bool `json:"mobile,omitempty"`
}

// Binding 动态链接绑定。
type Binding struct {
	Field string `json:"field,omitempty"`
}

// Props button 属性。
type Props struct {
	// Text 按钮文本（或绑定）。
	Text string `json:"text,omitempty" ct:"text,maxlen=200,sec=content"`
	// Binding 动态 CMS 链接绑定（如 post.permalink）。
	Binding *Binding `json:"binding,omitempty"`
	// Action 动作类型：internal/external/anchor/native/modal/link（默认 external）。
	Action string `json:"action,omitempty" ct:"select,internal=站内链接,external=外部链接,anchor=页内锚点,native=电话/邮件,modal=弹窗,link=自定义链接,default=external,sec=content,label=点击动作"`
	// Value 动作值：internal 站内路径 / external URL / anchor 元素ID / native tel-mailto / modal 目标ID。
	// 控件类型用 string 而非 safe：safe 走 IsSafeCSSValue 的 CSS 值白名单，其字符集
	// 刻意封禁 @（CSS 注入载体），而本字段是**链接值**——mailto:a@b.com 会被这条
	// CSS 规则误杀（实测报「字段 value 值非法」）。值域真源是下方 validateExtra 的
	// 按动作白名单（站内路径 / 外链协议 / 锚点 ID / tel-mailto 各自正则，未放宽），
	// 控件层只保留长度上限；href 输出侧由 jet.go 的 html.EscapeString 转义。
	Value string `json:"value,omitempty" ct:"string,maxlen=500,sec=content"`
	// Target 外部链接打开方式：self / blank（blank 自动 rel=noopener noreferrer）。
	Target string `json:"target,omitempty" ct:"select,self=当前窗口,blank=新窗口,sec=content,label=打开方式"`
	// Rel SEO 策略：none / nofollow / sponsored。
	Rel string `json:"rel,omitempty" ct:"select,none=默认,nofollow=加 nofollow,sponsored=赞助链接,sec=content,label=链接关系"`

	// --- 文本排版 ---
	FontSize      string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style"`
	FontWeight    string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,800=特粗,sec=style,label=字重"`
	LetterSpacing string `json:"letterSpacing,omitempty" ct:"dimension,maxlen=20,sec=style"`
	Transform     string `json:"transform,omitempty" ct:"select,none=无,uppercase=全大写,lowercase=全小写,capitalize=首字母大写,sec=style,label=大小写转换"`
	// Icon 图标配置（可选）。
	Icon *Icon `json:"icon,omitempty"`

	// --- 尺寸/变体/双态外观 ---
	Size string `json:"size,omitempty" ct:"select,xs=特小,sm=小,md=中,lg=大,xl=特大,default=md,sec=style,label=按钮尺寸"`
	// FontFamily 字体族（可选覆盖）。
	FontFamily string `json:"fontFamily,omitempty" ct:"safe,maxlen=200,sec=style,label=字体族"`
	// FullWidth 全宽按钮。
	FullWidth bool `json:"fullWidth,omitempty" ct:"bool,sec=style,label=全宽"`
	// --- 外观（扁平化：嵌套 State 无法在检查器直接编辑，故提升为顶层字段；
	//     编译时优先取扁平字段，缺省回退嵌套 State，兼容旧文档） ---
	// Bg 背景色（直接写 transparent / rgba(0,0,0,0) 即为透明底）。
	Bg string `json:"bg,omitempty" ct:"color,maxlen=200,sec=background,label=背景色"`
	// TextColor 文字颜色。
	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文字颜色"`
	// --- 边框与圆角（通用外观字段：所有组件同一命名，面板「边框」标签） ---
	// BorderWidth 边框宽度（空 = 不设边框，默认无边框）。
	BorderWidth string `json:"borderWidth,omitempty" ct:"dimension,maxlen=20,sec=border,label=边框宽度"`
	// BorderStyle 边框样式。
	BorderStyle string `json:"borderStyle,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,sec=border,label=边框样式"`
	// BorderColor 边框颜色。
	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=200,sec=border,label=边框颜色"`
	// RadiusTL/TR/BR/BL 四角圆角（空 = 0；四角全空时回退主题圆角）。
	RadiusTL string `json:"radiusTL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左上圆角"`
	RadiusTR string `json:"radiusTR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右上圆角"`
	RadiusBR string `json:"radiusBR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右下圆角"`
	RadiusBL string `json:"radiusBL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左下圆角"`
	// Shadow 阴影级别（sm/md/lg/xl）。
	Shadow string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=border,label=阴影"`
	// --- 悬停态（面板「悬停」标签） ---
	// HoverBorderColor 悬停边框色。
	HoverBorderColor string `json:"hoverBorderColor,omitempty" ct:"color,maxlen=200,sec=border,label=悬停边框色"`
	// HoverShadow 悬停阴影级别。
	HoverShadow string `json:"hoverShadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,sec=border,label=悬停阴影"`

	// HoverBg 悬停背景色。
	HoverBg string `json:"hoverBg,omitempty" ct:"color,maxlen=200,sec=background,label=悬停背景色"`
	// HoverColor 悬停文字色。
	HoverColor string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停文字色"`
	// LineHeight 行高（可选覆盖）。
	LineHeight string `json:"lineHeight,omitempty" ct:"dimension,maxlen=20,sec=style,label=行高"`
	Block      Block  `json:"block,omitempty"`
	Variant    string `json:"variant,omitempty" ct:"select,solid,outline,ghost,default=solid,sec=style"`
	// Normal 正常态；Hover 悬浮/聚焦态（缺省继承 Normal）。
	Normal State `json:"normal,omitempty"`
	Hover  State `json:"hover,omitempty"`
	// HoverLift 悬浮上浮距离（如 "-2px"；等价悬停态垂直偏移）。
	HoverLift string `json:"hoverLift,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停上浮"`
	// --- 变换（通用外观字段，面板「变换」标签；标准态） ---
	// Rotate 旋转角度（如 45deg）。
	Rotate string `json:"rotate,omitempty" ct:"dimension,maxlen=20,sec=transform,label=旋转"`
	// TranslateX / TranslateY 偏移。
	TranslateX string `json:"translateX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=水平偏移"`
	TranslateY string `json:"translateY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=垂直偏移"`
	// Scale 缩放倍数（如 1.1）。
	Scale string `json:"scale,omitempty" ct:"safe,maxlen=20,sec=transform,label=缩放"`
	// SkewX / SkewY 倾斜角度。
	SkewX string `json:"skewX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=倾斜 X"`
	SkewY string `json:"skewY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=倾斜 Y"`
	// FlipX / FlipY 翻转。
	FlipX bool `json:"flipX,omitempty" ct:"bool,sec=transform,label=水平翻转"`
	FlipY bool `json:"flipY,omitempty" ct:"bool,sec=transform,label=垂直翻转"`
	// --- 变换（悬停态） ---
	HoverRotate     string `json:"hoverRotate,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停旋转"`
	HoverTranslateX string `json:"hoverTranslateX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停水平偏移"`
	HoverTranslateY string `json:"hoverTranslateY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停垂直偏移"`
	HoverScale      string `json:"hoverScale,omitempty" ct:"safe,maxlen=20,sec=transform,label=悬停缩放"`
	HoverSkewX      string `json:"hoverSkewX,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停倾斜 X"`
	HoverSkewY      string `json:"hoverSkewY,omitempty" ct:"dimension,maxlen=20,sec=transform,label=悬停倾斜 Y"`
	// --- 通用动效（所有组件同一命名，面板「动效」标签） ---
	// TransitionDuration 过渡时长（如 0.2s / 200ms）。
	TransitionDuration string `json:"transitionDuration,omitempty" ct:"dimension,maxlen=20,sec=motion,label=过渡时长"`
	// TransitionEasing 缓动曲线。
	TransitionEasing string `json:"transitionEasing,omitempty" ct:"select,linear=线性,ease=缓入缓出,ease-in=缓入,ease-out=缓出,ease-in-out=先缓入再缓出,sec=motion,label=缓动曲线"`
	// TransitionDelay 过渡延迟（如 0.1s）。
	TransitionDelay string `json:"transitionDelay,omitempty" ct:"dimension,maxlen=20,sec=motion,label=过渡延迟"`

	// Loading 图标（媒体库图片）加载策略三态：空=默认（继承主题「图片管理」）/ on=开启 / off=关闭。
	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图标加载"`
	// FetchPriority 资源提示优先级：空=auto（不输出属性）/ high=首屏优先 / low=次要。
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"text"},
	},
}

var (
	internalPathRe = regexp.MustCompile(`^/[A-Za-z0-9/-]{0,200}$`)
	anchorIDRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	natualRe       = regexp.MustCompile(`^(tel:|mailto:)[^ \x00-\x20]{3,200}$`)
	fieldPathRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
)

// validateExtra 关系性校验：动作值/图标/绑定路径。
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
		// 绑定场景，无需 Value。
	default:
		return fmt.Errorf("无效的动作类型: %q", p.Action)
	}
	if p.Icon != nil {
		if p.Icon.Source == "builtin" {
			if _, ok := builtinIcons[p.Icon.Name]; !ok {
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

// buttonCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed button.css
var buttonCSS string

// compileCSS 按钮样式：尺寸 / 变体 / 双态 / 图标动效 / 块级。
//
// Go 侧只留「业务判定与兜底值计算」—— 尺寸查表、扁平字段回退嵌套 State、变体的边框
// 三分支、主题回退链、布尔开关；属性的组合方式与声明顺序全部在 button.css 里。
// 变体三分支（solid / outline / ghost）在样式源里是同一个声明块内的条件段，
// 命中的分支与基础声明合并成一条规则，与迁移前「按条件拼一个切片、只 Add 一次」等价。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	sizePadding, sizeFontSize := "10px 20px", "1rem"
	if preset, ok := sizePresets[p.Size]; ok {
		sizePadding, sizeFontSize = preset[0], preset[1]
	}

	// 大小写转换：空与 none 都表示「不输出」。
	textTransform := ""
	if p.Transform != "" && p.Transform != "none" {
		textTransform = p.Transform
	}

	// 圆角：四角全空回退主题级按钮圆角，否则四角独立（缺角补 0）。
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

	// 图标：间距 / 纵横排布 / 悬停位移（三者互不影响）。
	iconSpacing, iconShift := "", ""
	iconStacked, iconShiftOn := false, false
	if p.Icon != nil {
		iconSpacing = p.Icon.Spacing
		iconStacked = p.Icon.Position == "top" || p.Icon.Position == "bottom"
		iconShift = p.Icon.HoverShift
		iconShiftOn = p.Icon.HoverShift != ""
	}

	// 外观取值：扁平字段优先（检查器直接编辑），缺省回退嵌套 State（兼容旧文档）。
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

	// 变体归一：空与未知一律兜底实心（与迁移前 switch 的 default 分支一致）。
	variant := p.Variant
	switch variant {
	case VariantGhost, VariantOutline:
	default:
		variant = VariantSolid
	}

	// 各变体的背景取值：ghost / outline 透明底，显式设了背景（含 transparent）则尊重用户设置。
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

	// 悬浮 / 聚焦态：悬停扁平字段优先，缺省回退嵌套 Hover State，再回退正常态推导。
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
	// 一条悬停声明都没有时整段不产出（过渡也不产出）—— 与迁移前 len(hoverDecls) == 0 等价。
	hoverState := hoverBg != "" || hoverColor != "" || hoverBorder != "" ||
		hoverShadowValue != "" || hoverTransform != ""

	vars := map[string]string{
		"sizePadding":      sizePadding,
		"sizeFontSize":     sizeFontSize,
		"fontSize":         p.FontSize,
		"fontWeight":       p.FontWeight,
		"letterSpacing":    p.LetterSpacing,
		"textTransform":    textTransform,
		"radius":           radius,
		"fontFamily":       p.FontFamily,
		"lineHeight":       p.LineHeight,
		"fullWidth":        core.BoolVar(p.FullWidth),
		"iconSpacing":      iconSpacing,
		"iconStacked":      core.BoolVar(iconStacked),
		"variantGhost":     core.BoolVar(variant == VariantGhost),
		"ghostBg":          transparentBg,
		"ghostCompact":     core.BoolVar(p.Text == ""),
		"variantOutline":   core.BoolVar(variant == VariantOutline),
		"outlineBg":        transparentBg,
		"outlineBorder":    borderValue(p, borderColor, "1px solid currentColor"),
		"outlineColor":     textColor,
		"variantSolid":     core.BoolVar(variant == VariantSolid),
		"solidBg":          solidBg,
		"solidColor":       solidColor,
		"solidBorder":      solidBorder,
		"shadow":           shadow,
		"transform":        transformValue(p, false),
		"hoverBg":          p.HoverBg,
		"hoverColor":       p.HoverColor,
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
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("button 组件样式解析失败: %v", err))
	}
}

// borderValue 合成边框声明的值部分（宽度 / 样式 / 颜色，缺省 1px solid currentColor）。
// fallback 是该变体三分支都不命中时的兜底值（outline 与 solid 的回退链不同）。
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

// transformValue 合成 transform 声明的**值部分**（旋转 / 偏移 / 缩放 / 倾斜 / 翻转）。
// 属性名与声明位置由 button.css 给出，这里只算值 —— 「属性怎么组合」是样式源的事。
// hover=true 时取悬停态字段；悬停态全部未设置则返回空串（继承标准态，不覆盖）。
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

// transitionValue 通用过渡声明的**值部分**：时长 / 缓动 / 延迟，缺省 all 0.2s ease。
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

// isSafeURL 外链白名单（仅 http/https）。
// 有意比 core.IsSafeURL（允许 mailto/tel/#/相对路径）更严：external 动作只收外链，
// 站内路径/锚点/原生协议分别走 ActionInternal/ActionAnchor/ActionNative 独立分流，
// 此处不得放宽，否则相对路径会绕过 internal 的白名单正则。
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

// init 注册按钮组件。
func init() {
	core.Register(Widget)
}
