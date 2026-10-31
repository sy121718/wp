package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/internal/builder/core"
)

type ThemeSettings struct {
	Colors ThemeColors `json:"colors,omitempty"`

	Typography ThemeTypography `json:"typography,omitempty"`

	Button ThemeButton `json:"button,omitempty"`

	Surface ThemeSurface `json:"surface,omitempty"`

	Motion ThemeMotion `json:"motion,omitempty"`

	Images ThemeImages `json:"images,omitempty"`

	ThemeID string `json:"themeId,omitempty"`
}

type ThemeImages struct {
	LazyLoad string `json:"lazyLoad,omitempty"`

	Skeleton bool `json:"skeleton,omitempty"`
}

func (t *ThemeSettings) LazyLoadEnabled() bool {
	if t == nil {
		return true
	}
	return t.Images.LazyLoad != "off"
}

func (t *ThemeSettings) SkeletonEnabled() bool {
	return t != nil && t.Images.Skeleton
}

const reducedMotionCSS = `@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}`

func (t *ThemeSettings) ReducedMotionEnabled() bool {
	return t != nil && t.Motion.ReducedMotion
}

func (t *ThemeSettings) ReducedMotionCSS() string {
	return reducedMotionCSS
}

const viewTransitionsCSS = `@view-transition {
  navigation: auto;
}`

func (t *ThemeSettings) ViewTransitionsCSS() string {
	if t == nil || !t.Motion.ViewTransitions {
		return ""
	}
	return viewTransitionsCSS
}

func (t *ThemeSettings) RevealInheritOf() string {
	if t != nil && t.Motion.ScrollRevealDefault {
		return "on"
	}
	return ""
}

func (t *ThemeSettings) RevealDefaultEntranceOf() string {
	if t == nil || t.Motion.DefaultEntrance == "" {
		return "fade-in-up"
	}
	return t.Motion.DefaultEntrance
}

type ThemeColors struct {
	Primary    string `json:"primary,omitempty"`
	Secondary  string `json:"secondary,omitempty"`
	Accent     string `json:"accent,omitempty"`
	Success    string `json:"success,omitempty"`
	Warning    string `json:"warning,omitempty"`
	Danger     string `json:"danger,omitempty"`
	Text       string `json:"text,omitempty"`
	Heading    string `json:"heading,omitempty"`
	Background string `json:"background,omitempty"`
	Surface    string `json:"surface,omitempty"`
	Border     string `json:"border,omitempty"`
}

type ThemeTypography struct {
	Heading ThemeHeadingStyle `json:"heading,omitempty"`

	Body ThemeBodyStyle `json:"body,omitempty"`

	Link ThemeLinkStyle `json:"link,omitempty"`
}

type ThemeHeadingStyle struct {
	Color      string `json:"color,omitempty"`
	FontWeight string `json:"fontWeight,omitempty"`
	FontSize   string `json:"fontSize,omitempty"`
	Spacing    string `json:"spacing,omitempty"`
	FontFamily string `json:"fontFamily,omitempty"`
}

type ThemeBodyStyle struct {
	Color      string `json:"color,omitempty"`
	FontSize   string `json:"fontSize,omitempty"`
	LineHeight string `json:"lineHeight,omitempty"`
	FontFamily string `json:"fontFamily,omitempty"`
}

type ThemeLinkStyle struct {
	Color      string `json:"color,omitempty"`
	HoverColor string `json:"hoverColor,omitempty"`
	Underline  string `json:"underline,omitempty"`
}

type ThemeButton struct {
	Background      string `json:"background,omitempty"`
	Color           string `json:"color,omitempty"`
	Radius          string `json:"radius,omitempty"`
	FontWeight      string `json:"fontWeight,omitempty"`
	PaddingY        string `json:"paddingY,omitempty"`
	PaddingX        string `json:"paddingX,omitempty"`
	HoverBackground string `json:"hoverBackground,omitempty"`
	HoverColor      string `json:"hoverColor,omitempty"`

	BorderWidth string `json:"borderWidth,omitempty"`
	BorderStyle string `json:"borderStyle,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	Shadow      string `json:"shadow,omitempty"`
}

type ThemeSurface struct {
	Radius      string `json:"radius,omitempty"`
	BorderWidth string `json:"borderWidth,omitempty"`
	BorderColor string `json:"borderColor,omitempty"`
	Shadow      string `json:"shadow,omitempty"`
	Density     string `json:"density,omitempty"`
}

type ThemeMotion struct {
	TransitionDuration string `json:"transitionDuration,omitempty"`

	Easing string `json:"easing,omitempty"`

	DefaultEntrance string `json:"defaultEntrance,omitempty"`

	ReducedMotion bool `json:"reducedMotion,omitempty"`

	ViewTransitions bool `json:"viewTransitions,omitempty"`

	ScrollRevealDefault bool `json:"scrollRevealDefault,omitempty"`
}

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

	switch t.Surface.Density {
	case "compact":
		out = append(out, "--sky-density-pad: 8px", "--sky-density-gap: 16px", "--sky-density: compact")
	case "cozy":
		out = append(out, "--sky-density-pad: 24px", "--sky-density-gap: 32px", "--sky-density: cozy")
	}

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

	h := t.Typography.Heading
	add("heading-color", h.Color)
	add("heading-weight", h.FontWeight)
	add("heading-size", h.FontSize)
	add("heading-spacing", h.Spacing)
	add("heading-font", h.FontFamily)

	b := t.Typography.Body
	add("body-color", b.Color)
	add("body-size", b.FontSize)
	add("body-line", b.LineHeight)
	add("body-font", b.FontFamily)

	l := t.Typography.Link
	add("link-color", l.Color)
	add("link-hover", l.HoverColor)
	add("link-underline", l.Underline)

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

	s := t.Surface
	add("radius", s.Radius)
	add("border-width", s.BorderWidth)
	if s.BorderColor != "" {
		add("c-border", s.BorderColor)
	}
	add("shadow", s.Shadow)

	m := t.Motion
	if m.TransitionDuration != "" {
		add("tr-duration", m.TransitionDuration+"ms")
	}
	add("easing", m.Easing)
	return out
}

func ThemeVarsCSS(t *ThemeSettings) string {
	vars := themeVars(t)

	if id := themeIDSafe(t.GetThemeID()); id != "" {
		vars = append(vars, "--sky-theme-id: "+id)
	}
	if len(vars) == 0 {
		return ""
	}
	out := ":root{\n  " + strings.Join(vars, ";\n  ") + ";\n}"

	if strings.TrimSpace(t.Typography.Heading.FontFamily) != "" {
		out += "\nh1,h2,h3,h4,h5,h6{font-family:var(--sky-heading-font,inherit)}"
	}
	if strings.TrimSpace(t.Typography.Body.FontFamily) != "" {
		out += "\nbody{font-family:var(--sky-body-font,inherit)}"
	}

	if strings.TrimSpace(t.Typography.Body.Color) != "" {
		out += "\nbody{color:var(--sky-body-color,inherit)}"
	}

	if strings.TrimSpace(t.Colors.Background) != "" {
		out += "\nbody{background-color:var(--sky-c-bg,transparent)}"
	}
	return out
}

func (t *ThemeSettings) GetThemeID() string {
	if t == nil {
		return ""
	}
	return t.ThemeID
}

func themeIDSafe(id string) string {
	if id == "" {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return ""
		}
	}
	return `"` + id + `"`
}

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

func isSafeCSS(v string) bool {
	return core.IsSafeCSSValue(v)
}
