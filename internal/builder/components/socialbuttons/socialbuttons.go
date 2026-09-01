// Package socialbuttons 实现 core.social_buttons：社交图标组（对标 WD wd_social_buttons）。
// 内联 SVG 品牌图标，支持品牌色/灰色/自定义色，圆角/尺寸控制。
package socialbuttons

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.social_buttons"

func init() { core.Register(&Component{}) }

// Component 社交按钮组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Item 单个社交按钮。
type Item struct {
	// Platform 平台标识（facebook/x/instagram/youtube/tiktok/telegram/whatsapp/pinterest/linkedin）。
	Platform string `json:"platform,omitempty"`
	// URL 跳转地址。
	URL string `json:"url,omitempty"`
}

// ColorMode 配色：brand 品牌色 / mono 灰色 / custom 自定义。
type ColorMode string

const (
	ColorBrand   ColorMode = "brand"
	ColorMono    ColorMode = "mono"
	ColorCustom  ColorMode = "custom"
)

// Props 社交按钮属性。
type Props struct {
	// Items 按钮列表（repeater）。
	Items []Item `json:"items,omitempty"`
	// Color 配色模式：brand / mono / custom。
	Color ColorMode `json:"color,omitempty" ct:"select,brand=品牌色,mono=灰色,custom=自定义,default=brand,sec=style,label=配色"`
	// CustomColor 自定义图标颜色（color=custom 时）。
	CustomColor string `json:"customColor,omitempty" ct:"safe,maxlen=200,sec=style,label=图标颜色"`
	// Size 图标尺寸（px，默认 40）。
	Size string `json:"size,omitempty" ct:"safe,maxlen=20,sec=style,label=尺寸"`
	// Shape 形状：circle 圆形 / rounded 圆角 / square 方形。
	Shape string `json:"shape,omitempty" ct:"select,circle=圆形,rounded=圆角,square=方形,default=circle,sec=style,label=形状"`
	// Align 对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=对齐"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// brandColors 平台品牌色。
var brandColors = map[string]string{
	"facebook":  "#1877f2",
	"x":         "#000000",
	"instagram": "#e4405f",
	"youtube":   "#ff0000",
	"tiktok":    "#000000",
	"telegram":  "#229ed9",
	"whatsapp":  "#25d366",
	"pinterest": "#bd081c",
	"linkedin":  "#0a66c2",
	"threads":   "#000000",
	"vimeo":     "#1ab7ea",
	"flickr":    "#0063dc",
	"github":    "#181717",
	"dribbble":  "#ea4c89",
	"behance":   "#1769ff",
	"soundcloud": "#ff5500",
	"spotify":   "#1db954",
	"snapchat":  "#fffc00",
	"discord":   "#5865f2",
	"tumblr":    "#36465d",
	"viber":     "#7360f2",
	"vk":        "#0077ff",
	"bluesky":   "#0a7aff",
}

// platformIcons 平台内联 SVG（fill=currentColor）。
var platformIcons = map[string]string{
	"facebook": `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M22 12.06C22 6.5 17.52 2 12 2S2 6.5 2 12.06c0 5.02 3.66 9.18 8.44 9.94v-7.03H7.9v-2.91h2.54V9.85c0-2.52 1.49-3.91 3.77-3.91 1.09 0 2.24.2 2.24.2v2.47h-1.26c-1.24 0-1.63.78-1.63 1.57v1.88h2.78l-.45 2.9h-2.33V22c4.78-.75 8.44-4.9 8.44-9.94z"/></svg>`,
	"x":        `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M18.9 2H22l-6.77 7.74L23.2 22h-6.23l-4.88-6.38L6.5 22H3.36l7.24-8.28L2.8 2h6.39l4.41 5.83L18.9 2zm-1.09 18.13h1.73L7.1 3.75H5.25l12.56 16.38z"/></svg>`,
	"instagram": `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M12 2.16c3.2 0 3.58.01 4.85.07 3.25.15 4.77 1.69 4.92 4.92.06 1.27.07 1.65.07 4.85s-.01 3.58-.07 4.85c-.15 3.23-1.66 4.77-4.92 4.92-1.27.06-1.64.07-4.85.07s-3.58-.01-4.85-.07c-3.26-.15-4.77-1.7-4.92-4.92C2.17 15.58 2.16 15.2 2.16 12s.01-3.58.07-4.85C2.38 3.92 3.9 2.38 7.15 2.23 8.42 2.17 8.8 2.16 12 2.16zm0 3.68a6.16 6.16 0 100 12.32 6.16 6.16 0 000-12.32zm0 10.16a4 4 0 110-8 4 4 0 010 8zm6.4-11.85a1.44 1.44 0 100 2.88 1.44 1.44 0 000-2.88z"/></svg>`,
	"youtube":  `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M23.5 6.19a3.02 3.02 0 00-2.12-2.14C19.5 3.55 12 3.55 12 3.55s-7.5 0-9.38.5A3.02 3.02 0 00.5 6.19C0 8.07 0 12 0 12s0 3.93.5 5.81a3.02 3.02 0 002.12 2.14c1.88.5 9.38.5 9.38.5s7.5 0 9.38-.5a3.02 3.02 0 002.12-2.14C24 15.93 24 12 24 12s0-3.93-.5-5.81zM9.55 15.57V8.43L15.82 12l-6.27 3.57z"/></svg>`,
	"tiktok":   `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M19.59 6.69a4.83 4.83 0 01-3.77-4.25V2h-3.45v13.67a2.9 2.9 0 01-5.2 1.74 2.89 2.89 0 012.31-4.64 2.93 2.93 0 01.88.13V9.4a6.84 6.84 0 00-1-.05A6.33 6.33 0 005 20.1a6.34 6.34 0 0010.86-4.43v-7a8.16 8.16 0 004.77 1.52v-3.4a4.85 4.85 0 01-1-.1z"/></svg>`,
	"telegram": `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M11.94 2A10 10 0 1022 12 10 10 0 0011.94 2zm4.7 6.83l-1.62 7.63c-.12.55-.45.68-.91.42l-2.5-1.85-1.2 1.16a.63.63 0 01-.5.24l.18-2.54 4.63-4.18c.2-.18-.04-.28-.31-.1l-5.72 3.6-2.47-.77c-.54-.17-.55-.54.11-.8l9.65-3.72c.45-.17.84.11.67.91z"/></svg>`,
	"whatsapp": `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M17.47 14.38c-.3-.15-1.76-.87-2.03-.97-.27-.1-.47-.15-.67.15-.2.3-.77.97-.94 1.17-.17.2-.35.22-.65.07-.3-.15-1.26-.46-2.4-1.48a9.06 9.06 0 01-1.66-2.06c-.17-.3-.02-.46.13-.61.14-.13.3-.35.45-.52.15-.17.2-.3.3-.5.1-.2.05-.37-.02-.52-.08-.15-.67-1.62-.92-2.22-.24-.58-.49-.5-.67-.51h-.57c-.2 0-.52.07-.8.37-.27.3-1.04 1.02-1.04 2.5 0 1.47 1.07 2.89 1.22 3.09.15.2 2.1 3.2 5.1 4.49.71.3 1.27.49 1.7.62.72.23 1.37.2 1.88.12.58-.09 1.76-.72 2-1.42.25-.7.25-1.3.18-1.42-.08-.12-.28-.2-.57-.34zM12.05 21.8h-.01a9.87 9.87 0 01-5.03-1.38l-.36-.21-3.74.98 1-3.65-.24-.37a9.86 9.86 0 01-1.51-5.26c0-5.45 4.44-9.88 9.9-9.88a9.84 9.84 0 019.89 9.9c0 5.44-4.44 9.87-9.9 9.87zm8.42-18.3A11.82 11.82 0 0012.05 0C5.5 0 .16 5.34.16 11.9c0 2.1.55 4.14 1.59 5.95L.06 24l6.3-1.65a11.9 11.9 0 005.68 1.45h.01c6.55 0 11.89-5.34 11.89-11.9a11.82 11.82 0 00-3.47-8.4z"/></svg>`,
	"pinterest": `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M12 2C6.48 2 2 6.48 2 12c0 4.24 2.64 7.86 6.36 9.31-.09-.79-.17-2.01.03-2.87.19-.79 1.21-5.02 1.21-5.02s-.31-.62-.31-1.54c0-1.44.84-2.52 1.88-2.52.89 0 1.32.67 1.32 1.47 0 .89-.57 2.23-.86 3.47-.25 1.04.52 1.88 1.54 1.88 1.85 0 3.27-1.95 3.27-4.76 0-2.49-1.79-4.23-4.34-4.23-2.96 0-4.69 2.22-4.69 4.51 0 .89.34 1.85.77 2.37.08.1.1.19.07.29-.08.32-.25 1.02-.28 1.16-.05.19-.16.23-.36.14-1.34-.62-2.18-2.59-2.18-4.17 0-3.39 2.47-6.51 7.11-6.51 3.73 0 6.63 2.66 6.63 6.21 0 3.71-2.34 6.7-5.58 6.7-1.09 0-2.11-.57-2.46-1.24l-.67 2.55c-.24.93-.9 2.1-1.34 2.81A9.98 9.98 0 0012 22c5.52 0 10-4.48 10-10S17.52 2 12 2z"/></svg>`,
	"linkedin": `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><path d="M20.45 20.45h-3.55v-5.57c0-1.33-.03-3.04-1.85-3.04-1.86 0-2.14 1.45-2.14 2.94v5.67H9.35V9h3.41v1.56h.05c.47-.9 1.63-1.85 3.36-1.85 3.6 0 4.27 2.37 4.27 5.46v6.28zM5.34 7.43a2.06 2.06 0 110-4.12 2.06 2.06 0 010 4.12zM7.12 20.45H3.56V9h3.56v11.45z"/></svg>`,
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 社交按钮为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	for _, it := range p.Items {
		if _, ok := platformIcons[it.Platform]; !ok {
			return fmt.Errorf("节点 %s: 未知社交平台 %q", node.ID, it.Platform)
		}
	}
	switch p.Color {
	case "", ColorBrand, ColorMono, ColorCustom:
	default:
		return fmt.Errorf("节点 %s: 无效的配色模式 %q", node.ID, p.Color)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

// Render 渲染社交按钮组。
func (c *Component) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	cls := core.NodeClass(node.ID)
	color := p.Color
	if color == "" {
		color = ColorBrand
	}

	ctx.HTML.WriteString(`<div class="`)
	ctx.HTML.WriteString(cls)
	ctx.HTML.WriteString(` wp-social wp-social-`)
	ctx.HTML.WriteString(string(color))
	ctx.HTML.WriteString(`">`)
	for _, it := range p.Items {
		svg, ok := platformIcons[it.Platform]
		ctx.HTML.WriteString(`<a class="wp-social-btn" href="`)
		ctx.HTML.WriteString(html.EscapeString(it.URL))
		ctx.HTML.WriteString(`" target="_blank" rel="noopener nofollow" aria-label="`)
		ctx.HTML.WriteString(html.EscapeString(it.Platform))
		ctx.HTML.WriteString(`">`)
		if ok {
			ctx.HTML.WriteString(svg)
		} else {
			// 无专属 SVG 的平台：首字母圆形兜底（品牌色已按平台映射）。
			ctx.HTML.WriteString(`<span class="wp-social-fallback">`)
			ctx.HTML.WriteString(html.EscapeString(strings.ToUpper(it.Platform[:1])))
			ctx.HTML.WriteString(`</span>`)
		}
		ctx.HTML.WriteString(`</a>`)
	}
	ctx.HTML.WriteString(`</div>`)

	compileCSS(node.ID, &p, ctx.CSS)
	return nil
}

// compileCSS 社交按钮样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	align := p.Align
	if align == "" {
		align = "left"
	}
	justify := "flex-start"
	if align == "center" {
		justify = "center"
	} else if align == "right" {
		justify = "flex-end"
	}
	size := p.Size
	if size == "" {
		size = "40px"
	}

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex", "gap: 8px", "justify-content: " + justify,
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-social-btn", []string{
		"width: " + size, "height: " + size,
		"display: inline-flex", "align-items: center", "justify-content: center",
		"font-size: calc(" + size + " * 0.55)",
		"text-decoration: none",
		"transition: transform .15s, opacity .15s",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-social-btn:hover", []string{"transform: translateY(-2px)"})

	// 形状。
	shape := p.Shape
	if shape == "" {
		shape = "circle"
	}
	switch shape {
	case "rounded":
		b.Add(core.BreakpointDesktop, sel+" .wp-social-btn", []string{"border-radius: 10px"})
	case "square":
		b.Add(core.BreakpointDesktop, sel+" .wp-social-btn", []string{"border-radius: 0"})
	default:
		b.Add(core.BreakpointDesktop, sel+" .wp-social-btn", []string{"border-radius: 999px"})
	}

	// 配色。
	switch p.Color {
	case ColorMono:
		b.Add(core.BreakpointDesktop, sel+" .wp-social-btn", []string{
			"color: #6b7280", "background: rgba(0,0,0,.06)",
		})
	case ColorCustom:
		col := p.CustomColor
		if col == "" {
			col = "#2563eb"
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-social-btn", []string{
			"color: " + col, "background: rgba(0,0,0,.06)",
		})
	default: // brand
		var rules []string
		for platform, color := range brandColors {
			cls := sel + " a[aria-label=\"" + platform + "\"]"
			b.Add(core.BreakpointDesktop, cls, []string{"color: #fff", "background: " + color})
		}
		_ = rules
	}
}
