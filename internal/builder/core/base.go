package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ---------- 共享白名单（全组件统一，含容器） ----------

var (
	// SafeValueRe CSS 值白名单：字母数字与常见安全符号，禁止引号/分号/花括号/@/反斜杠/尖括号等注入载体。
	// 注意：括号整体保留 —— rgba() 等合法 CSS 函数值（容器 Overlay 等控件的既有取值）依赖括号，
	// 无法一刀切禁止；url() 外联注入风险由 cssExternalURLRe 单独封禁（见 IsSafeCSSValue）。
	SafeValueRe = regexp.MustCompile(`^[A-Za-z0-9#%.,()\-+/:?=&_~ ]*$`)
	// cssExternalURLRe CSS 值中的外联资源引用：url() 内以 // 或 http(s):// 开头的绝对外部地址
	// （background-image: url(//attacker) 外联注入载体）。大小写不敏感，兼容 url( 与 url (、
	// 引号包裹等 CSS 语法变体；站内相对路径 url(/img/a.jpg) 不受影响。
	cssExternalURLRe = regexp.MustCompile(`(?i)url\s*\(\s*['"]?\s*(?:https?:|//)`)
	// CustomClassRe 自定义 class 白名单：禁止 sky- 前缀之外的注入字符（sky- 前缀由 ValidateAdvanced 单独拦截）。
	CustomClassRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
	// CustomIDRe 自定义 Element ID 白名单（锚点）。
	CustomIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	// LengthValueRe 四向间距单值白名单：CSS 长度/auto/百分比，允许负值（微叠放），单侧下限见 negMarginLimit。
	LengthValueRe = regexp.MustCompile(`^-?[A-Za-z0-9.%]+$`)
)

// 阴影预设 Token（与容器 02-A 的 shadowLevels 对齐，此处为通用层的权威定义）。
var ShadowPresets = map[string]string{
	"sm": "0 1px 3px rgba(0,0,0,0.12)",
	"md": "0 4px 12px rgba(0,0,0,0.12)",
	"lg": "0 10px 28px rgba(0,0,0,0.16)",
	"xl": "0 20px 48px rgba(0,0,0,0.2)",
	// neon 霓虹发光（强调元素/暗色主题；双层光晕，颜色与 --sky-c-primary 同族）。
	"neon": "0 0 8px rgba(59,130,246,.6), 0 0 24px rgba(59,130,246,.35)",
}

// negMarginLimit 负边距下限（单侧绝对值上限，防溢出视口）。
const negMarginLimit = 300

// zIndexLimit z-index 边界（负边距叠放所需的层级控制）。
const zIndexLimit = 100

// hueRotateLimit 色相偏移角度边界（滤镜度数上限，超出无视觉意义）。
const hueRotateLimit = 360

// IsSafeCSSValue CSS 值白名单校验（长度上限 500）。全组件共用的唯一入口。
// 收紧取舍说明（M 级 url() 外联注入修复）：SafeValueRe 字符集保留括号（rgba() 等
// 合法函数取值依赖，见容器 Overlay 控件），故不做「仅放行 ^url\(站内路径\)$」的
// 全量收紧，而是单独封禁外联形式：url(//…) 与 url(http(s)://…) 一律拒绝，
// 站内相对路径 url(/img/a.jpg) 与函数值 rgba(…) 保持既有行为。
func IsSafeCSSValue(v string) bool {
	return len(v) <= 500 && !cssExternalURLRe.MatchString(v) && SafeValueRe.MatchString(v)
}

// ---------- 结构化四向值（面板四输入框 + 锁定联动的数据模型） ----------

// Spacing 四向独立值。空字符串表示未设置；四值全空视为未配置。
// 支持锁定联动（编辑器面板行为，等值即可）与负值（限幅见 negMarginLimit）。
type Spacing struct {
	Top    string `json:"top,omitempty"`
	Right  string `json:"right,omitempty"`
	Bottom string `json:"bottom,omitempty"`
	Left   string `json:"left,omitempty"`
}

// IsEmpty 四向全空。
func (s Spacing) IsEmpty() bool {
	return s.Top == "" && s.Right == "" && s.Bottom == "" && s.Left == ""
}

// CSS 拼接为 CSS 简写值（top right bottom left）；全空返回空串。
func (s Spacing) CSS() string {
	if s.IsEmpty() {
		return ""
	}
	return strings.Join([]string{s.Top, s.Right, s.Bottom, s.Left}, " ")
}

// ResponsiveSpacing 三端独立间距。
type ResponsiveSpacing struct {
	Desktop Spacing `json:"desktop,omitempty"`
	Tablet  Spacing `json:"tablet,omitempty"`
	Mobile  Spacing `json:"mobile,omitempty"`
}

// validateSpacing 校验四向值：长度白名单 + 负值限幅。
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
			// 负值限幅：提取数值前缀（数字与小数点，截止到单位开始处）判断。
			// 原实现用 TrimRight 字符集裁剪单位，无法覆盖 q/vh 等未知单位，
			// 非法单位会使 ParseFloat 失败而静默放行，绕过限幅。
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

// cssDeclsUnsafeRe CSS 声明里的危险字符（注入/跳出规则）。
var cssDeclsUnsafeRe = regexp.MustCompile("[{}<>\"'\x60]")

// IsSafeCSSDecls 校验「分号分隔的 CSS 声明」：允许 : ; , . % # ( ) - / 空格与字母数字，
// 拒绝大括号/引号/尖括号等可跳出规则的字符；长度上限 500。
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

// parseResponsiveDecls 解析「分号分隔的 CSS 声明」为声明列表。
// 只接受 "prop: value" 形式且通过安全白名单，非法片段静默丢弃（构建期不 panic）。
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

// ---------- 通用高级属性（原子组件 Advanced 层，规范 docs/02-C0） ----------

// WidthMode 自身宽度模式。
const (
	WidthAuto  = "auto"  // 自适应内容
	WidthFull  = "full"  // 铺满父容器 100%
	WidthFixed = "fixed" // 固定自定义宽度
)

// alignSelfMap Align Self 关键字到 CSS 值。
var alignSelfMap = map[string]string{
	"start": "flex-start", "center": "center", "end": "flex-end",
	"stretch": "stretch", "baseline": "baseline", "auto": "auto",
}

// AdvancedProps 原子组件通用高级属性。所有原子组件（Heading/Text/Button/Image...）
// 在自身专属 Props 之外统一嵌入本结构（json 字段名 advanced）。
type AdvancedProps struct {
	// Margin 外边距：四向独立 + 三端响应式，支持负值（限幅）做微叠放。
	Margin ResponsiveSpacing `json:"margin,omitempty" ct:"spacing,sec=layout,label=外距"`
	// Padding 内边距：四向独立 + 三端响应式（按钮/图文块等内留白组件）。
	Padding ResponsiveSpacing `json:"padding,omitempty" ct:"spacing,sec=layout,label=内距"`
	// WidthMode 自身宽度：auto / full / fixed（默认 auto）。
	WidthMode string `json:"widthMode,omitempty" ct:"select,auto=自适应,full=铺满父容器,fixed=固定宽度,sec=layout,label=宽度模式"`
	// WidthValue fixed 模式下的自定义宽度（如 "320px"）。
	WidthValue string `json:"widthValue,omitempty" ct:"dimension,maxlen=20,sec=layout,label=固定宽度"`
	// AlignSelf 在 Flex 容器中的自身对齐，覆盖父容器统一对齐。
	AlignSelf string `json:"alignSelf,omitempty" ct:"select,auto=默认,start=起始,center=居中,end=末端,stretch=拉伸,baseline=基线,sec=layout,label=自对齐"`
	// Border 边框（三要素需同时提供才生效）；ct:"group" 展开到面板「边框」区块。
	Border BorderProps `json:"border,omitempty" ct:"group"`
	// Radius 四角独立圆角（顺时针：左上/右上/右下/左下），用于不规则圆角。
	Radius RadiusProps `json:"radius,omitempty" ct:"group"`
	// Shadow 阴影预设 Token：sm / md / lg / xl / neon（霓虹发光，强调元素）。
	Shadow string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,neon=霓虹,sec=border,label=阴影"`
	// Surface 表面质感预设："" 标准 / glass 玻璃拟态 / liquid 液态玻璃。
	// 需要元素背后有内容（背景图/相邻区块）才呈现透镜效果；见 core/effects.go。
	Surface string `json:"surface,omitempty" ct:"select,=标准,glass=玻璃拟态,liquid=液态玻璃,neumorph=新拟态,sec=border,label=表面质感"`
	// TextGradient 渐变文字（CSS 渐变值；background-clip: text 实现）。
	TextGradient string `json:"textGradient,omitempty" ct:"safe,maxlen=300,sec=advanced,label=渐变文字"`
	// Opacity 不透明度 0~100（百分比）。
	Opacity int `json:"opacity,omitempty" ct:"int,min=0,max=100,sec=layout,label=不透明度(%)"`
	// HueRotate 色相偏移（deg，-360~360，0=不偏移）：整体调色，或做多元素色相轮转
	// （同一结构复制多份、每份给不同角度，即可拼出色相渐变的卡片/图标墙）。
	// 编译为 filter: hue-rotate(Ndeg)。
	//
	// 注意 filter 是**单值属性**：同元素上若还有其它 filter 效果，后者覆盖前者，
	// 不会叠加。当前库内作用于同元素的 filter 只有「入场 blur-in 动画」——
	// 动画播放期间由 keyframes 接管、结束后回落，属预期行为。
	HueRotate int `json:"hueRotate,omitempty" ct:"int,min=-360,max=360,sec=style,label=色相偏移(deg)"`
	// HideOn 响应式显隐开关：三端全开时编译器照常输出（保持哑与确定性），编辑器层提示。
	HideOn HideOn `json:"hideOn,omitempty" ct:"group"`
	// ZIndex 层级（负边距叠放控制），[-100, 100]。
	ZIndex int `json:"zIndex,omitempty" ct:"int,min=-100,max=100,sec=layout,label=Z-index"`
	// Interaction 交互/动效组（入场动画/延迟/悬浮上浮/吸顶），全组件共享。
	Interaction InteractionProps `json:"interaction,omitempty"`
	// TabletCSS 平板端覆盖声明（分号分隔的 CSS 声明，如 "font-size:16px;padding:12px"）。
	// 通用按端覆盖：任何属性都能在指定断点覆盖桌面值，避免为每个字段都做三端变体。
	TabletCSS string `json:"tabletCss,omitempty" ct:"cssdecls,maxlen=500,sec=responsive,label=平板端样式覆盖"`
	// MobileCSS 手机端样式覆盖（同上：只写样式/布局/动画属性）。
	MobileCSS string `json:"mobileCss,omitempty" ct:"cssdecls,maxlen=500,sec=responsive,label=手机端样式覆盖"`

	// CustomClasses 自定义 class（禁 sky- 前缀，防碰撞编译产物命名空间）。
	CustomClasses []string `json:"customClasses,omitempty" ct:"classes,sec=advanced,label=CSS 类"`
	// CustomID 自定义 Element ID（锚点跳转），全文档唯一（复用节点 ID 查重 map）。
	CustomID string `json:"customId,omitempty" ct:"safe,maxlen=64,sec=advanced,label=CSS ID"`
}

// BorderProps 边框三要素（面板「边框」区块，留空 = 无边框）。
type BorderProps struct {
	Width string `json:"width,omitempty" ct:"dimension,maxlen=20,sec=border,label=边框宽度"` // 如 "1px"
	Style string `json:"style,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,sec=border,label=边框样式"`
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=border,label=边框颜色"`
	// Gradient 渐变边框（border-image 方案，优先于三要素边框；与 Radius 同用时圆角失效为直角）。
	Gradient string `json:"gradient,omitempty" ct:"safe,maxlen=300,sec=border,label=渐变边框"`
	// Flow 边框流动（Gradient 需为 conic-gradient(...)，角度旋转动画）。
	Flow bool `json:"flow,omitempty" ct:"bool,sec=border,label=边框流动"`
}

// IsSet 边框是否已配置。
func (b BorderProps) IsSet() bool {
	return b.Width != "" || b.Style != "" || b.Color != ""
}

// RadiusProps 四角独立圆角（面板「边框」区块，四角合并为一个带联动锁的控件）。
type RadiusProps struct {
	TopLeft     string `json:"topLeft,omitempty" ct:"dimension,maxlen=20,sec=border,label=左上圆角"`
	TopRight    string `json:"topRight,omitempty" ct:"dimension,maxlen=20,sec=border,label=右上圆角"`
	BottomRight string `json:"bottomRight,omitempty" ct:"dimension,maxlen=20,sec=border,label=右下圆角"`
	BottomLeft  string `json:"bottomLeft,omitempty" ct:"dimension,maxlen=20,sec=border,label=左下圆角"`
}

// IsEmpty 四角全空。
func (r RadiusProps) IsEmpty() bool {
	return r.TopLeft == "" && r.TopRight == "" && r.BottomRight == "" && r.BottomLeft == ""
}

// CSS 拼接四角圆角简写。
func (r RadiusProps) CSS() string {
	if r.IsEmpty() {
		return ""
	}
	return strings.Join([]string{r.TopLeft, r.TopRight, r.BottomRight, r.BottomLeft}, " ")
}

// HideOn 响应式显隐开关（勾选 = 该端隐藏）。
type HideOn struct {
	Desktop bool `json:"desktop,omitempty" ct:"bool,sec=layout,label=桌面隐藏"`
	Tablet  bool `json:"tablet,omitempty" ct:"bool,sec=layout,label=平板隐藏"`
	Mobile  bool `json:"mobile,omitempty" ct:"bool,sec=layout,label=手机隐藏"`
}

// IsEmpty 无任何隐藏。
func (h HideOn) IsEmpty() bool {
	return !h.Desktop && !h.Tablet && !h.Mobile
}

// allowedBorderStyle 边框线型白名单。
var allowedBorderStyle = map[string]bool{
	"solid": true, "dashed": true, "dotted": true, "double": true,
}

// ValidateAdvanced 校验通用高级属性（全原子组件共用一份规则）。
// nodeID 仅用于错误定位。customID 非空时登记进 ids 保证全文档唯一。
func ValidateAdvanced(a *AdvancedProps, nodeID string, ids map[string]bool) (err error) {
	// 交互/动效组校验（entrance 白名单 + delay 限幅）。
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
	// 表面质感白名单（效果基本库，core/effects.go）。
	if !allowedSurface[a.Surface] {
		return fmt.Errorf("节点 %s: 无效的表面质感: %q（glass/liquid）", nodeID, a.Surface)
	}
	// 渐变边框：CSS 安全值；流动需 conic-gradient（否则角度动画无消费对象）。
	if a.Border.Gradient != "" {
		if !IsSafeCSSValue(a.Border.Gradient) {
			return fmt.Errorf("节点 %s: 无效的渐变边框: %q", nodeID, a.Border.Gradient)
		}
		if a.Border.Flow && !strings.Contains(a.Border.Gradient, "conic-gradient(") {
			return fmt.Errorf("节点 %s: 边框流动需 conic-gradient 渐变", nodeID)
		}
	}
	// 渐变文字安全值。
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

// CompileAdvanced 将通用高级属性编译为三端 CSS 规则（全原子组件共用一份生成逻辑）。
// 输出追加到 buckets；返回值传给渲染层：附加 class 列表与自定义 Element ID。
func CompileAdvanced(nodeID string, a *AdvancedProps, b *CSSBuckets) (extraClasses []string, customID string) {
	sel := "." + NodeClass(nodeID)

	var desktop, tablet, mobile []string

	// 间距：部分设置时输出长属性（margin-top 等），避免简写空槽改变语义；四值全设才输出简写。
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

	// 宽度与对齐。
	switch a.WidthMode {
	case WidthFull:
		desktop = append(desktop, "width: 100%")
	case WidthFixed:
		desktop = append(desktop, "width: "+a.WidthValue)
	}
	if a.AlignSelf != "" && a.AlignSelf != "auto" {
		desktop = append(desktop, "align-self: "+alignSelfMap[a.AlignSelf])
	}

	// 边框与圆角。
	if a.Border.IsSet() {
		desktop = append(desktop, "border: "+a.Border.Width+" "+a.Border.Style+" "+a.Border.Color)
	}
	if v := a.Radius.CSS(); v != "" {
		desktop = append(desktop, "border-radius: "+v)
	}
	// 渐变边框（border-image 方案，优先于三要素边框；border-width 缺省 2px；
	// 与 Radius 同用时圆角失效为直角，注释已告知）。流动 = conic 角度旋转动画。
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

	// 阴影 / 不透明度 / 层级。
	if v, ok := ShadowPresets[a.Shadow]; a.Shadow != "" && ok {
		desktop = append(desktop, "box-shadow: "+v)
	}
	// 表面质感（glass/liquid，效果基本库 core/effects.go；内阴影覆盖上方外阴影预设，质感优先）。
	CompileSurface(sel, a.Surface, b)
	// 渐变文字（覆盖 color/背景类声明，置于尾部保证优先级）。
	if a.TextGradient != "" {
		desktop = append(desktop, TextGradientDecls(a.TextGradient)...)
	}
	if a.Opacity > 0 && a.Opacity < 100 {
		desktop = append(desktop, fmt.Sprintf("opacity: %s", strconv.FormatFloat(float64(a.Opacity)/100, 'f', 2, 64)))
	}
	// 色相偏移（filter: hue-rotate）：整体调色能力。
	// 放在 Shadow/Surface 之后、ZIndex 之前：与它们无属性重叠，位置仅影响可读性。
	if a.HueRotate != 0 {
		desktop = append(desktop, fmt.Sprintf("filter: hue-rotate(%ddeg)", a.HueRotate))
	}
	if a.ZIndex != 0 {
		desktop = append(desktop, fmt.Sprintf("z-index: %d", a.ZIndex))
	} else if a.Interaction.Sticky {
		// 吸顶自动抬升层叠（10）：吸顶元素需高于后续内容，否则会被滚动上来的
		// 区块覆盖；用户显式设置 ZIndex 时以其为准（此处不覆盖）。
		// 放在 Advanced 而非 CompileInteraction：后者单独成规则，会覆盖用户 ZIndex。
		desktop = append(desktop, "z-index: 10")
	}

	// 通用按端覆盖：解析「分号分隔的 CSS 声明」追加到对应断点（后者覆盖前者，符合 CSS 层叠）。
	tablet = append(tablet, parseResponsiveDecls(a.TabletCSS)...)
	mobile = append(mobile, parseResponsiveDecls(a.MobileCSS)...)

	b.Add(BreakpointDesktop, sel, desktop)
	b.Add(BreakpointTablet, sel, tablet)
	b.Add(BreakpointMobile, sel, mobile)

	// 响应式显隐：desktop-first，桌面隐藏直接输出，平板/手机进对应媒体查询。
	if a.HideOn.Desktop {
		b.Add(BreakpointDesktop, sel, []string{"display: none"})
	}
	if a.HideOn.Tablet {
		b.Add(BreakpointTablet, sel, []string{"display: none"})
	}
	if a.HideOn.Mobile {
		b.Add(BreakpointMobile, sel, []string{"display: none"})
	}

	// 交互/动效组（入场动画/延迟/悬浮上浮/吸顶），全组件共享（docs/06 §6 同源管线）。
	CompileInteraction(sel, a.Interaction, b)

	return a.CustomClasses, a.CustomID
}

// spacingDecls 生成单端间距声明（tablet/mobile 复用长属性逻辑）。
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
