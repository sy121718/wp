// Package builder — 主题设置模型与 CSS 变量编译（Woodmart 级全局默认）。
//
// ThemeSettings 是主题的全局设计令牌：色板/排版/按钮/链接/边框/圆角/动效。
// 编译期生成 :root CSS 变量块注入产物 <head>——主题色真正进产物（替代
// 组件硬编码默认色），全站组件经 var(--wp-c-*) 引用主题令牌。
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
}

// ThemeSurface 全局表面。
type ThemeSurface struct {
	Radius      string `json:"radius,omitempty"`      // 全局圆角
	BorderWidth string `json:"borderWidth,omitempty"` // 全局边框宽
	BorderColor string `json:"borderColor,omitempty"` // 全局边框色（Colors.Border 别名）
	Shadow      string `json:"shadow,omitempty"`      // 默认阴影级别 sm/md/lg
}

// ThemeMotion 动效全局默认。
type ThemeMotion struct {
	// TransitionDuration 全局过渡时长（ms 数值字符串，如 "200"）。
	TransitionDuration string `json:"transitionDuration,omitempty"`
	// Easing 全局缓动：ease / ease-out / linear。
	Easing string `json:"easing,omitempty"`
	// DefaultEntrance 新组件默认入场动画（继承效果基本库）。
	DefaultEntrance string `json:"defaultEntrance,omitempty"`
}

// themeVar 主题变量映射表（Go 字段 → CSS 变量名 + 值），未设置字段跳过。
func themeVars(t *ThemeSettings) []string {
	if t == nil {
		return nil
	}
	var out []string
	add := func(name, val string) {
		if val != "" {
			out = append(out, fmt.Sprintf("--wp-%s: %s", name, val))
		}
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
	// 表面。
	s := t.Surface
	add("radius", s.Radius)
	add("border-width", s.BorderWidth)
	if s.BorderColor != "" {
		add("c-border", s.BorderColor)
	}
	add("shadow", s.Shadow)
	// 动效。
	m := t.Motion
	add("tr-duration", m.TransitionDuration+"ms")
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
