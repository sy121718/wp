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
	// CustomClassRe 自定义 class 白名单：禁止 wp- 前缀之外的注入字符（wp- 前缀由 ValidateAdvanced 单独拦截）。
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
}

// negMarginLimit 负边距下限（单侧绝对值上限，防溢出视口）。
const negMarginLimit = 300

// zIndexLimit z-index 边界（负边距叠放所需的层级控制）。
const zIndexLimit = 100

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
			// 负值限幅：提取数值部分判断（单位可为 px/em/rem/% 等）。
			numStr := strings.TrimRight(strings.TrimLeft(v, "-"), "pxemremit%.")
			if f, e := strconv.ParseFloat(numStr, 64); e == nil && f > negMarginLimit {
				return fmt.Errorf("%s%s负值超出下限 -%dpx: %q", name, side, negMarginLimit, v)
			}
		}
	}
	return nil
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
	Margin ResponsiveSpacing `json:"margin,omitempty"`
	// Padding 内边距：四向独立 + 三端响应式（按钮/图文块等内留白组件）。
	Padding ResponsiveSpacing `json:"padding,omitempty"`
	// WidthMode 自身宽度：auto / full / fixed（默认 auto）。
	WidthMode string `json:"widthMode,omitempty"`
	// WidthValue fixed 模式下的自定义宽度（如 "320px"）。
	WidthValue string `json:"widthValue,omitempty"`
	// AlignSelf 在 Flex 容器中的自身对齐，覆盖父容器统一对齐。
	AlignSelf string `json:"alignSelf,omitempty"`
	// Border 边框（三要素需同时提供才生效）。
	Border BorderProps `json:"border,omitempty"`
	// Radius 四角独立圆角（顺时针：左上/右上/右下/左下），用于不规则圆角。
	Radius RadiusProps `json:"radius,omitempty"`
	// Shadow 阴影预设 Token：sm / md / lg / xl。
	Shadow string `json:"shadow,omitempty"`
	// Opacity 不透明度 0~100（百分比）。
	Opacity int `json:"opacity,omitempty"`
	// HideOn 响应式显隐开关：三端全开时编译器照常输出（保持哑与确定性），编辑器层提示。
	HideOn HideOn `json:"hideOn,omitempty"`
	// ZIndex 层级（负边距叠放控制），[-100, 100]。
	ZIndex int `json:"zIndex,omitempty"`
	// CustomClasses 自定义 class（禁 wp- 前缀，防碰撞编译产物命名空间）。
	CustomClasses []string `json:"customClasses,omitempty"`
	// CustomID 自定义 Element ID（锚点跳转），全文档唯一（复用节点 ID 查重 map）。
	CustomID string `json:"customId,omitempty"`
}

// BorderProps 边框三要素。
type BorderProps struct {
	Width string `json:"width,omitempty"` // 如 "1px"
	Style string `json:"style,omitempty"` // solid / dashed / dotted / double
	Color string `json:"color,omitempty"`
}

// IsSet 边框是否已配置。
func (b BorderProps) IsSet() bool {
	return b.Width != "" || b.Style != "" || b.Color != ""
}

// RadiusProps 四角独立圆角。
type RadiusProps struct {
	TopLeft     string `json:"topLeft,omitempty"`
	TopRight    string `json:"topRight,omitempty"`
	BottomRight string `json:"bottomRight,omitempty"`
	BottomLeft  string `json:"bottomLeft,omitempty"`
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

// HideOn 响应式显隐开关。
type HideOn struct {
	Desktop bool `json:"desktop,omitempty"`
	Tablet  bool `json:"tablet,omitempty"`
	Mobile  bool `json:"mobile,omitempty"`
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
	if a.Opacity < 0 || a.Opacity > 100 {
		return fmt.Errorf("节点 %s: 不透明度必须在 0~100 之间: %d", nodeID, a.Opacity)
	}
	if a.ZIndex < -zIndexLimit || a.ZIndex > zIndexLimit {
		return fmt.Errorf("节点 %s: z-index 必须在 [-%d, %d] 之间: %d", nodeID, zIndexLimit, zIndexLimit, a.ZIndex)
	}

	for _, cls := range a.CustomClasses {
		if !CustomClassRe.MatchString(cls) {
			return fmt.Errorf("节点 %s: 无效的自定义 class: %q", nodeID, cls)
		}
		if strings.HasPrefix(cls, "wp-") {
			return fmt.Errorf("节点 %s: 自定义 class 禁止使用 wp- 保留前缀: %q", nodeID, cls)
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

	// 阴影 / 不透明度 / 层级。
	if v, ok := ShadowPresets[a.Shadow]; a.Shadow != "" && ok {
		desktop = append(desktop, "box-shadow: "+v)
	}
	if a.Opacity > 0 && a.Opacity < 100 {
		desktop = append(desktop, fmt.Sprintf("opacity: %s", strconv.FormatFloat(float64(a.Opacity)/100, 'f', 2, 64)))
	}
	if a.ZIndex != 0 {
		desktop = append(desktop, fmt.Sprintf("z-index: %d", a.ZIndex))
	}

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
