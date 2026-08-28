package builder

import (
	"errors"
	"fmt"
	"regexp"

	containerPkg "go_wp/internal/builder/components/container"
	"go_wp/internal/builder/core"
)

// 版心模式常量。
const (
	LayoutBoxed = "boxed" // 定宽居中
	LayoutFull  = "full"  // 全宽铺满
)

// body 基础 class。
const (
	bodyClassPage  = "wp-page"
	bodyClassBoxed = "wp-boxed"
	bodyClassFull  = "wp-full"
)

// bodyClassRe body 自定义 class 白名单。
var bodyClassRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// PageSettings 页面级全局环境配置（规范 docs/02-A §2）。
// 不是可视化 DOM 节点，由独立的"页面设置面板"维护，编译期直接作用于 <head> 与 <body>。
type PageSettings struct {
	Layout      PageLayout `json:"layout"`
	Base        BaseStyle  `json:"base"`
	Theme       ThemeSettings `json:"theme,omitempty"`
	SEO         SEO        `json:"seo"`
	BodyClasses []string   `json:"bodyClasses,omitempty"`
	// Structure 全局结构绑定快照（保存时从激活主题 settings 合入）：
	// 编译装配层读取，构建期内联页眉/页脚块（021_blocks.sql 方案 C）。
	Structure StructureBindings `json:"structure,omitempty"`
}

// StructureBindings 页面对全局块的槽位绑定快照。
type StructureBindings struct {
	// HeaderBlockID 页眉全局块 ID（空 = 无页眉）。
	HeaderBlockID string `json:"headerBlockId,omitempty"`
	// FooterBlockID 页脚全局块 ID（空 = 无页脚）。
	FooterBlockID string `json:"footerBlockId,omitempty"`
}

// PageLayout 页面版心控制。
type PageLayout struct {
	// Mode 布局模式：boxed 定宽居中 / full 全宽铺满。
	Mode string `json:"mode"`
	// MaxWidth 定宽模式下的最大内容宽度，如 "1200px"。
	MaxWidth string `json:"maxWidth,omitempty"`
	// SafePadding 三端最小安全左右留白，防小屏贴边。
	SafePadding struct {
		Desktop string `json:"desktop,omitempty"`
		Tablet  string `json:"tablet,omitempty"`
		Mobile  string `json:"mobile,omitempty"`
	} `json:"safePadding,omitempty"`
}

// BaseStyle 整页基底样式，注入 <body>。
type BaseStyle struct {
	BackgroundColor string `json:"backgroundColor,omitempty"`
	// BackgroundImage 背景图 URL，默认平铺。
	BackgroundImage string `json:"backgroundImage,omitempty"`
	// BackgroundFixed 固定背景（background-attachment: fixed）。
	BackgroundFixed bool `json:"backgroundFixed,omitempty"`
}

// SEO SEO 与全局元信息。
type SEO struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// validateSettings 校验页面设置。
func validateSettings(s *PageSettings) (err error) {
	switch s.Layout.Mode {
	case LayoutBoxed, LayoutFull:
	default:
		return fmt.Errorf("无效的版心模式: %q", s.Layout.Mode)
	}

	// 定宽模式必须给定最大内容宽度。
	if s.Layout.Mode == LayoutBoxed {
		if s.Layout.MaxWidth == "" {
			return errors.New("定宽模式下必须设置最大内容宽度")
		}
		if !safeCSS(s.Layout.MaxWidth) {
			return fmt.Errorf("无效的最大内容宽度值: %q", s.Layout.MaxWidth)
		}
	}

	for bp, v := range map[string]string{
		"desktop": s.Layout.SafePadding.Desktop,
		"tablet":  s.Layout.SafePadding.Tablet,
		"mobile":  s.Layout.SafePadding.Mobile,
	} {
		if v != "" && !safeCSS(v) {
			return fmt.Errorf("无效的 %s 端安全留白值: %q", bp, v)
		}
	}

	if !safeCSS(s.Base.BackgroundColor) {
		return fmt.Errorf("无效的背景颜色值: %q", s.Base.BackgroundColor)
	}
	if s.Base.BackgroundImage != "" && !safeCSS(s.Base.BackgroundImage) {
		return fmt.Errorf("无效的背景图地址: %q", s.Base.BackgroundImage)
	}

	if len(s.SEO.Title) > 200 {
		return errors.New("页面标题过长（上限 200 字符）")
	}
	if len(s.SEO.Description) > 500 {
		return errors.New("页面描述过长（上限 500 字符）")
	}

	for _, cls := range s.BodyClasses {
		if !bodyClassRe.MatchString(cls) {
			return fmt.Errorf("无效的 body 自定义 class: %q", cls)
		}
	}
	return nil
}

// safeCSS 复用容器组件导出的 CSS 值白名单校验。
func safeCSS(v string) bool {
	return containerPkg.IsSafeCSSValue(v)
}

// compileSettingsCSS 编译页面设置为 CSS：body 基底样式与版心约束。
func compileSettingsCSS(s *PageSettings, b *core.CSSBuckets) {
	// 主题 Token → :root CSS 变量（组件统一用 var(--color-*) 取色）。
	var root []string
	if v := s.Theme.Colors.Primary; v != "" {
		root = append(root, "--color-primary: "+v)
	}
	if v := s.Theme.Colors.Text; v != "" {
		root = append(root, "--color-text: "+v)
	}
	if v := s.Theme.Colors.Background; v != "" {
		root = append(root, "--color-bg: "+v)
	}
	if v := s.Theme.Colors.Surface; v != "" {
		root = append(root, "--color-surface: "+v)
	}
	if v := s.Theme.Colors.Border; v != "" {
		root = append(root, "--color-border: "+v)
	}
	if len(root) > 0 {
		b.Add(core.BreakpointDesktop, ":root", root)
	}
	if v := s.Theme.FontFamily; v != "" {
		b.Add(core.BreakpointDesktop, "body", []string{"font-family: "+v})
	}
	var body []string
	if v := s.Base.BackgroundColor; v != "" {
		body = append(body, "background-color: "+v)
	}
	if v := s.Base.BackgroundImage; v != "" {
		body = append(body, "background-image: url("+v+")")
	}
	if s.Base.BackgroundFixed {
		body = append(body, "background-attachment: fixed")
	}
	b.Add(core.BreakpointDesktop, "body", body)

	if s.Layout.Mode != LayoutBoxed {
		return
	}

	// 版心：顶级 Section 定宽居中 + 三端安全留白。
	var sec []string
	sec = append(sec, "max-width: "+s.Layout.MaxWidth)
	sec = append(sec, "margin-left: auto")
	sec = append(sec, "margin-right: auto")
	if v := s.Layout.SafePadding.Desktop; v != "" {
		sec = append(sec, "padding-left: "+v)
		sec = append(sec, "padding-right: "+v)
	}
	b.Add(core.BreakpointDesktop, "body."+bodyClassBoxed+" ."+core.SectionClass, sec)

	if v := s.Layout.SafePadding.Tablet; v != "" {
		b.Add(core.BreakpointTablet, "body."+bodyClassBoxed+" ."+core.SectionClass, []string{
			"padding-left: " + v, "padding-right: " + v,
		})
	}
	if v := s.Layout.SafePadding.Mobile; v != "" {
		b.Add(core.BreakpointMobile, "body."+bodyClassBoxed+" ."+core.SectionClass, []string{
			"padding-left: " + v, "padding-right: " + v,
		})
	}
}

// ThemeSettings 站点级设计 Token（来自 project settings，保存页面/改主题时合入页面文档）。
type ThemeSettings struct {
	Colors struct {
		Primary    string `json:"primary,omitempty"`
		Text       string `json:"text,omitempty"`
		Background string `json:"background,omitempty"`
		Surface    string `json:"surface,omitempty"`
		Border     string `json:"border,omitempty"`
	} `json:"colors,omitempty"`
	// FontFamily 正文字体栈。
	FontFamily string `json:"fontFamily,omitempty"`
}
