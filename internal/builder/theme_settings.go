// Package builder — 主题设置模型与 CSS 变量编译（Woodmart 级全局默认）。
//
// ThemeSettings 是主题的全局设计令牌：色板/排版/按钮/链接/边框/圆角/动效。
// 编译期生成 :root CSS 变量块注入产物 <head>——主题色真正进产物（替代
// 组件硬编码默认色），全站组件经 var(--sky-c-*) 引用主题令牌。
//
// 设计原则（docs/06 §6 同源）：
//   - 全部值经 core.IsSafeCSSValue 白名单校验（防注入）；
//   - 未设置的字段不输出变量（组件回退到自身默认值）；
//   - 确定性：同一 ThemeSettings 产生相同变量块字节。
package builder

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// ThemeSettings 主题全局设置（Woodmart 级）。
type ThemeSettings struct {
	// Colors 色板。
	Colors ThemeColors `json:"colors,omitempty"`
	// Typography 排版默认（标题/正文/链接）。
	Typography ThemeTypography `json:"typography,omitempty"`
	// Button 按钮全局默认。
	Button ThemeButton `json:"button,omitempty"`
	// Surface 全局表面（背景/边框/圆角）。
	Surface ThemeSurface `json:"surface,omitempty"`
	// Motion 动效全局默认。
	Motion ThemeMotion `json:"motion,omitempty"`
	// Images 图片全局默认（懒加载策略 + 骨架屏），组件未显式设置时继承。
	Images ThemeImages `json:"images,omitempty"`
}

// ThemeImages 图片全局默认（主题「图片管理」）。
type ThemeImages struct {
	// LazyLoad 懒加载默认策略：on（开启，默认）/ off（关闭）。
	// 组件级 loading 为空（默认）时继承此值。
	LazyLoad string `json:"lazyLoad,omitempty"`
	// Skeleton 懒加载图片显示骨架屏（纯 CSS 渐变占位，图片加载完成后自然覆盖）。
	Skeleton bool `json:"skeleton,omitempty"`
}

// LazyLoadEnabled 解析主题懒加载默认值（未设置视为开启）。
func (t *ThemeSettings) LazyLoadEnabled() bool {
	if t == nil {
		return true
	}
	return t.Images.LazyLoad != "off"
}

// SkeletonEnabled 主题是否开启骨架屏。
func (t *ThemeSettings) SkeletonEnabled() bool {
	return t != nil && t.Images.Skeleton
}

// reducedMotionCSS 减弱动态效果无障碍块（标准实现，覆盖组件与插件全部动画/过渡）。
const reducedMotionCSS = `@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}`

// ReducedMotionEnabled 主题是否显式开启「减弱动态效果」跟随系统（历史字段，默认 false）。
func (t *ThemeSettings) ReducedMotionEnabled() bool {
	return t != nil && t.Motion.ReducedMotion
}

// ReducedMotionCSS 无障碍块：无条件输出 prefers-reduced-motion 规则。
// 媒体查询本身只在访客系统偏好开启时生效，不影响其它用户；主题开关不再 gate 输出。
func (t *ThemeSettings) ReducedMotionCSS() string {
	return reducedMotionCSS
}

// viewTransitionsCSS 页面转场规则（跨文档导航自动转场，默认交叉淡化）。
const viewTransitionsCSS = `@view-transition {
  navigation: auto;
}`

// ViewTransitionsCSS 页面转场规则（未开启返回空，产物字节不变）。
// 同源多页跳转生效（静态站天然同源）；不支持 @view-transition 的浏览器
// 忽略规则降级为普通跳转。
func (t *ThemeSettings) ViewTransitionsCSS() string {
	if t == nil || !t.Motion.ViewTransitions {
		return ""
	}
	return viewTransitionsCSS
}

// RevealInheritOf 滚动显现继承初值（"on" = 全站滚动显现；其余 = 未启用）。
func (t *ThemeSettings) RevealInheritOf() string {
	if t != nil && t.Motion.ScrollRevealDefault {
		return "on"
	}
	return ""
}

// RevealDefaultEntranceOf 滚动显现注入的默认入场词（空 = fade-up）。
func (t *ThemeSettings) RevealDefaultEntranceOf() string {
	if t == nil || t.Motion.DefaultEntrance == "" {
		return "fade-up"
	}
	return t.Motion.DefaultEntrance
}

// ThemeColors 色板令牌。
type ThemeColors struct {
	Primary    string `json:"primary,omitempty"`   // 主色（按钮/链接/强调）
	Secondary  string `json:"secondary,omitempty"` // 次色
	Accent     string `json:"accent,omitempty"`    // 点缀色（徽章/标签）
	Success    string `json:"success,omitempty"`
	Warning    string `json:"warning,omitempty"`
	Danger     string `json:"danger,omitempty"`
	Text       string `json:"text,omitempty"`       // 正文色
	Heading    string `json:"heading,omitempty"`    // 标题色
	Background string `json:"background,omitempty"` // 页面背景
	Surface    string `json:"surface,omitempty"`    // 卡片/面板背景
	Border     string `json:"border,omitempty"`     // 全局边框色
}

// ThemeTypography 排版默认。
type ThemeTypography struct {
	// Heading 标题排版。
	Heading ThemeHeadingStyle `json:"heading,omitempty"`
	// Body 正文。
	Body ThemeBodyStyle `json:"body,omitempty"`
	// Link 链接。
	Link ThemeLinkStyle `json:"link,omitempty"`
}

// ThemeHeadingStyle 标题全局默认。
type ThemeHeadingStyle struct {
	Color      string `json:"color,omitempty"`
	FontWeight string `json:"fontWeight,omitempty"`
	FontSize   string `json:"fontSize,omitempty"` // 基准字号（h2 基准）
	Spacing    string `json:"spacing,omitempty"`  // 标题下间距
	FontFamily string `json:"fontFamily,omitempty"`
}

// ThemeBodyStyle 正文全局默认。
type ThemeBodyStyle struct {
	Color      string `json:"color,omitempty"`
	FontSize   string `json:"fontSize,omitempty"`
	LineHeight string `json:"lineHeight,omitempty"`
	FontFamily string `json:"fontFamily,omitempty"`
}

// ThemeLinkStyle 链接全局默认。
type ThemeLinkStyle struct {
	Color      string `json:"color,omitempty"`
	HoverColor string `json:"hoverColor,omitempty"`
	Underline  string `json:"underline,omitempty"` // none / hover / always
}

// ThemeButton 按钮全局默认。
type ThemeButton struct {
	Background      string `json:"background,omitempty"`
	Color           string `json:"color,omitempty"` // 文字色
	Radius          string `json:"radius,omitempty"`
	FontWeight      string `json:"fontWeight,omitempty"`
	PaddingY        string `json:"paddingY,omitempty"` // 纵向内边距
	PaddingX        string `json:"paddingX,omitempty"`
	HoverBackground string `json:"hoverBackground,omitempty"`
	HoverColor      string `json:"hoverColor,omitempty"`
	// 边框与阴影（全局按钮默认，组件级可覆盖）。
	BorderWidth string `json:"borderWidth,omitempty"`
	BorderStyle string `json:"borderStyle,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	Shadow      string `json:"shadow,omitempty"`
}

// ThemeSurface 全局表面。
type ThemeSurface struct {
	Radius      string `json:"radius,omitempty"`      // 全局圆角
	BorderWidth string `json:"borderWidth,omitempty"` // 全局边框宽
	BorderColor string `json:"borderColor,omitempty"` // 全局边框色（Colors.Border 别名）
	Shadow      string `json:"shadow,omitempty"`      // 默认阴影级别 sm/md/lg
	Density     string `json:"density,omitempty"`     // 密度档位：空=标准 / compact 紧凑 / cozy 宽松
	// Density 密度档位："" 标准（内边距 16px / 间距 24px）/ compact 紧凑（8/16）/ cozy 宽松（24/32）。
	// 编译为语义变量（--sky-density-pad / --sky-density-gap）+ 样式查询开关（--sky-density），
	// 组件经 var() 消费即可「一处切换全站间距」；也可用 @container style(--sky-density: compact)
	// 做结构性差异（如紧凑模式下卡片改横排）。
}

// ThemeMotion 动效全局默认。
type ThemeMotion struct {
	// TransitionDuration 全局过渡时长（ms 数值字符串，如 "200"）。
	TransitionDuration string `json:"transitionDuration,omitempty"`
	// Easing 全局缓动：ease / ease-out / linear。
	Easing string `json:"easing,omitempty"`
	// DefaultEntrance 新组件默认入场动画（继承效果基本库）。
	DefaultEntrance string `json:"defaultEntrance,omitempty"`
	// ReducedMotion 尊重系统「减弱动态效果」（prefers-reduced-motion）：
	// 开启时产物注入无障碍块，全部动画/过渡压至近零时长（WCAG 2.3.3；
	// H5 移动端系统开关生效，动画敏感用户零动效浏览）。
	ReducedMotion bool `json:"reducedMotion,omitempty"`
	// ViewTransitions 页面转场（@view-transition，跨文档导航自动淡入淡出）：
	// 同源静态多页跳转自带 Apple 式丝滑转场；不支持的浏览器降级为普通跳转。
	ViewTransitions bool `json:"viewTransitions,omitempty"`
	// ScrollRevealDefault 全站滚动显现（H5「滚动过去才出内容」）：开启后，
	// 未显式配置入场的组件自动注入 DefaultEntrance + view() 滚动触发；
	// 容器可在子树内豁免（StyleEx.Reveal=off）。老浏览器降级为直接显示。
	ScrollRevealDefault bool `json:"scrollRevealDefault,omitempty"`
}

// themeVar 主题变量映射表（Go 字段 → CSS 变量名 + 值），未设置字段跳过。
func themeVars(t *ThemeSettings) []string {
	if t == nil {
		return nil
	}
	var out []string
	add := func(name, val string) {
		if val != "" {
			out = append(out, fmt.Sprintf("--sky-%s: %s", name, val))
		}
	}
	// 密度档位：语义间距变量 + 样式查询开关（组件经 var() 消费，一处切换全站；
	// @container style(--sky-density: compact) 供组件做结构性差异）。未设置则不输出。
	switch t.Surface.Density {
	case "compact":
		out = append(out, "--sky-density-pad: 8px", "--sky-density-gap: 16px", "--sky-density: compact")
	case "cozy":
		out = append(out, "--sky-density-pad: 24px", "--sky-density-gap: 32px", "--sky-density: cozy")
	}
	// 色板。
	c := t.Colors
	add("c-primary", c.Primary)
	add("c-secondary", c.Secondary)
	add("c-accent", c.Accent)
	add("c-success", c.Success)
	add("c-warning", c.Warning)
	add("c-danger", c.Danger)
	add("c-text", c.Text)
	add("c-heading", c.Heading)
	add("c-bg", c.Background)
	add("c-surface", c.Surface)
	add("c-border", c.Border)
	// 标题排版。
	h := t.Typography.Heading
	add("heading-color", h.Color)
	add("heading-weight", h.FontWeight)
	add("heading-size", h.FontSize)
	add("heading-spacing", h.Spacing)
	add("heading-font", h.FontFamily)
	// 正文。
	b := t.Typography.Body
	add("body-color", b.Color)
	add("body-size", b.FontSize)
	add("body-line", b.LineHeight)
	add("body-font", b.FontFamily)
	// 链接。
	l := t.Typography.Link
	add("link-color", l.Color)
	add("link-hover", l.HoverColor)
	add("link-underline", l.Underline)
	// 按钮。
	btn := t.Button
	add("btn-bg", btn.Background)
	add("btn-color", btn.Color)
	add("btn-radius", btn.Radius)
	add("btn-weight", btn.FontWeight)
	add("btn-py", btn.PaddingY)
	add("btn-px", btn.PaddingX)
	add("btn-hover-bg", btn.HoverBackground)
	add("btn-hover-color", btn.HoverColor)
	add("btn-border-width", btn.BorderWidth)
	add("btn-border-style", btn.BorderStyle)
	add("btn-border-color", btn.BorderColor)
	add("btn-shadow", btn.Shadow)
	// 表面。
	s := t.Surface
	add("radius", s.Radius)
	add("border-width", s.BorderWidth)
	if s.BorderColor != "" {
		add("c-border", s.BorderColor)
	}
	add("shadow", s.Shadow)
	// 动效（空值不输出，避免 --sky-tr-duration: ms 这类非法声明）。
	m := t.Motion
	if m.TransitionDuration != "" {
		add("tr-duration", m.TransitionDuration+"ms")
	}
	add("easing", m.Easing)
	return out
}

// ThemeVarsCSS 编译主题设置为 :root 变量块（注入产物 head）。
// 空主题返回空串（不输出空块）。所有值已在字段层经 IsSafeCSSValue 约束。
func ThemeVarsCSS(t *ThemeSettings) string {
	vars := themeVars(t)
	if len(vars) == 0 {
		return ""
	}
	return ":root{\n  " + strings.Join(vars, ";\n  ") + ";\n}"
}

// ValidateThemeSettings 校验主题设置全部值（IsSafeCSSValue 白名单）。
func ValidateThemeSettings(t *ThemeSettings) (err error) {
	if t == nil {
		return nil
	}
	check := func(name, val string) error {
		if val != "" && !isSafeCSS(val) {
			return fmt.Errorf("主题设置 %s 值非法: %q", name, val)
		}
		return nil
	}
	c := t.Colors
	for k, v := range map[string]string{
		"主色": c.Primary, "次色": c.Secondary, "点缀色": c.Accent,
		"成功色": c.Success, "警告色": c.Warning, "危险色": c.Danger,
		"正文色": c.Text, "标题色": c.Heading, "背景色": c.Background,
		"面板色": c.Surface, "边框色": c.Border,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	h := t.Typography.Heading
	for k, v := range map[string]string{
		"标题颜色": h.Color, "标题字重": h.FontWeight, "标题字号": h.FontSize,
		"标题间距": h.Spacing, "标题字体": h.FontFamily,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	b := t.Typography.Body
	for k, v := range map[string]string{
		"正文颜色": b.Color, "正文字号": b.FontSize, "正文行高": b.LineHeight, "正文字体": b.FontFamily,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	l := t.Typography.Link
	for k, v := range map[string]string{
		"链接颜色": l.Color, "链接悬停色": l.HoverColor, "链接下划线": l.Underline,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	btn := t.Button
	for k, v := range map[string]string{
		"按钮背景": btn.Background, "按钮文字色": btn.Color, "按钮圆角": btn.Radius,
		"按钮字重": btn.FontWeight, "按钮纵向内边距": btn.PaddingY, "按钮横向内边距": btn.PaddingX,
		"按钮悬停背景": btn.HoverBackground, "按钮悬停文字色": btn.HoverColor,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	s := t.Surface
	for k, v := range map[string]string{
		"全局圆角": s.Radius, "全局边框宽": s.BorderWidth, "全局边框色": s.BorderColor, "默认阴影": s.Shadow,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	m := t.Motion
	for k, v := range map[string]string{
		"过渡时长": m.TransitionDuration, "缓动": m.Easing, "默认入场": m.DefaultEntrance,
	} {
		if err := check(k, v); err != nil {
			return err
		}
	}
	return nil
}

// ParseThemeSettings 从 JSON 解析并校验。
func ParseThemeSettings(data json.RawMessage) (t *ThemeSettings, err error) {
	if len(data) == 0 {
		return &ThemeSettings{}, nil
	}
	t = &ThemeSettings{}
	if err = json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("主题设置解析失败: %w", err)
	}
	if err = ValidateThemeSettings(t); err != nil {
		return nil, err
	}
	return t, nil
}

// isSafeCSS 简洁别名（委托 core.IsSafeCSSValue，含 url 外联注入封禁）。
func isSafeCSS(v string) bool {
	return core.IsSafeCSSValue(v)
}
