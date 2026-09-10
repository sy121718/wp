package builder

import (
	"errors"
	"fmt"
	"regexp"

	"go_wp/internal/builder/core"
)

// 版心模式常量。
const (
	LayoutBoxed = "boxed" // 定宽居中
	LayoutFull  = "full"  // 全宽铺满
)

// body 基础 class。
const (
	bodyClassPage  = "sky-page"
	bodyClassBoxed = "sky-boxed"
	bodyClassFull  = "sky-full"
)

// bodyClassRe body 自定义 class 白名单。
var bodyClassRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// PageSettings 页面级全局环境配置（规范 docs/02-A §2）。
// 不是可视化 DOM 节点，由独立的"页面设置面板"维护，编译期直接作用于 <head> 与 <body>。
type PageSettings struct {
	Layout      PageLayout     `json:"layout"`
	Base        BaseStyle      `json:"base"`
	Theme       *ThemeSettings `json:"theme,omitempty"`
	SEO         SEO            `json:"seo"`
	BodyClasses []string       `json:"bodyClasses,omitempty"`
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
	// FocusKeyword 主关键词（SEO 评分用，不参与产物）。
	FocusKeyword string `json:"focusKeyword,omitempty"`
	// SecondaryKeywords 次级关键词（评分用）。
	SecondaryKeywords []string `json:"secondaryKeywords,omitempty"`
	// Canonical 规范链接（空 = 由 URL 推导）。
	Canonical string `json:"canonical,omitempty"`
	// OGImage 社交分享图。
	OGImage string `json:"ogImage,omitempty"`
	// Intent 查询意图（评分的内容长度分档：informational/commercial/transactional/local/definition）。
	Intent string `json:"intent,omitempty"`
	// SchemaType 结构化数据类型：空=自动（WebPage）/website/article/product/faq。
	SchemaType string `json:"schemaType,omitempty"`
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

// safeCSS CSS 值白名单校验（委托 core.IsSafeCSSValue，含 url 外联注入封禁）。
func safeCSS(v string) bool {
	return core.IsSafeCSSValue(v)
}

// compileSettingsCSS 编译页面设置为 CSS：body 基底样式与版心约束。
// 主题令牌统一由 ThemeVarsCSS 生成 --sky-c-* 变量（theme_settings.go，
// 完整 11 色 + 排版 + 按钮 + 表面 + 动效），此处只应用 body 字体/背景等
// 页面级规则，不再重复输出旧的 --color-* 变量。
func compileSettingsCSS(s *PageSettings, b *core.CSSBuckets) {
	if s.Theme != nil {
		// 正文字体（主题排版默认应用进 body）。
		if v := s.Theme.Typography.Body.FontFamily; v != "" {
			b.Add(core.BreakpointDesktop, "body", []string{"font-family: " + v})
		}
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
	// body 同时作为样式查询锚点（container-name 不启用 containment → 零布局影响）：
	// 主题/插件在 body 上声明的语义开关可被任意组件用 @container style() 响应。
	body = append(body, "container-name: sky-theme")
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
