// Package infobox 实现 core.infobox：信息框组件（对标 WD wd_infobox）。
// 图标（或媒体图）+ 标题 + 文本 + 可选链接，常用于服务/卖点卡片。
package infobox

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.infobox"

func init() { core.Register(&Component{}) }

// Component 信息框组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Props 信息框属性。
type Props struct {
	// Icon 内置图标名（check/star/arrow/shield/truck/cross 等；与 MediaImage 二选一）。
	Icon string `json:"icon,omitempty" ct:"select,,check=对勾,star=星形,arrow=箭头,shield=盾牌,truck=卡车,cross=叉形,sec=content,label=图标"`
	// MediaImage 媒体图 URL（与 Icon 二选一，优先于 Icon）。
	MediaImage string `json:"mediaImage,omitempty" ct:"media,sec=content,label=图片"`
	// Title 标题。
	Title string `json:"title,omitempty" ct:"text,maxlen=200,sec=content,label=标题"`
	// Text 描述文本。
	Text string `json:"text,omitempty" ct:"text,maxlen=2000,sec=content,label=描述"`
	// Link 整卡链接（可选）。
	Link string `json:"link,omitempty" ct:"url,sec=content,label=链接"`
	// IconColor 图标颜色。
	IconColor string `json:"iconColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`
	// TitleColor 标题颜色。
	TitleColor string `json:"titleColor,omitempty" ct:"color,maxlen=200,sec=style,label=标题颜色"`
	// TextColor 文本颜色。
	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文本颜色"`
	// Align 内容对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`
	// IconSize 图标尺寸（px，默认 40）。
	IconSize string `json:"iconSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=图标尺寸"`
	// Padding 内边距（CSS 简写）。
	Padding string `json:"padding,omitempty" ct:"margin,maxlen=30,sec=style,label=内边距"`
	// Background 卡片背景色。
	Background string `json:"background,omitempty" ct:"color,maxlen=200,sec=style,label=背景色"`
	// HoverBg 悬停背景色。
	HoverBg string `json:"hoverBg,omitempty" ct:"color,maxlen=200,sec=style,label=悬停背景色"`
	// Radius 圆角。
	Radius string `json:"radius,omitempty" ct:"safe,maxlen=30,sec=style,label=圆角"`
	// Subtitle 副标题（图标与标题之间）。
	Subtitle string `json:"subtitle,omitempty" ct:"text,maxlen=200,sec=content,label=副标题"`
	// SubtitleColor 副标题颜色。
	SubtitleColor string `json:"subtitleColor,omitempty" ct:"color,maxlen=200,sec=style,label=副标题颜色"`
	// IconBgColor 图标背景色。
	IconBgColor string `json:"iconBgColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标背景色"`
	// IconBorderColor 图标边框色。
	IconBorderColor string `json:"iconBorderColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标边框色"`
	// TitleTag 标题标签（h2/h3/h4/div，默认 h3）。
	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=H2,h3=H3,h4=H4,div=区块,default=h3,sec=style,label=标题标签"`
	// BtnText 按钮文字（与 Link 配合）。
	BtnText string `json:"btnText,omitempty" ct:"safe,maxlen=100,sec=content,label=按钮文字"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 信息框为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.Icon != "" {
		if _, ok := core.IconSVG(p.Icon); !ok {
			return fmt.Errorf("节点 %s: 未知图标 %q", node.ID, p.Icon)
		}
	}
	switch p.Align {
	case "", "left", "center", "right":
	default:
		return fmt.Errorf("节点 %s: 无效的对齐 %q", node.ID, p.Align)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

// Render 渲染信息框。
func (c *Component) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	cls := core.NodeClass(node.ID)

	body := strings.Builder{}
	// 图标 / 媒体图。
	if p.MediaImage != "" {
		body.WriteString(`<span class="wp-infobox-media">`)
		body.WriteString(`<img src="`)
		body.WriteString(html.EscapeString(p.MediaImage))
		body.WriteString(`" alt="" loading="lazy">`)
		body.WriteString(`</span>`)
	} else if p.Icon != "" {
		body.WriteString(`<span class="wp-infobox-icon">`)
		if svg, ok := core.IconSVG(p.Icon); ok {
			body.WriteString(svg)
		}
		body.WriteString(`</span>`)
	}
	if p.Subtitle != "" {
		body.WriteString(`<span class="wp-infobox-subtitle">`)
		body.WriteString(html.EscapeString(p.Subtitle))
		body.WriteString(`</span>`)
	}
	if p.Title != "" {
		tag := p.TitleTag
		if tag == "" {
			tag = "h3"
		}
		body.WriteString("<" + tag + ` class="wp-infobox-title">`)
		body.WriteString(html.EscapeString(p.Title))
		body.WriteString("</" + tag + ">")
	}
	if p.Text != "" {
		body.WriteString(`<div class="wp-infobox-text">`)
		body.WriteString(html.EscapeString(p.Text))
		body.WriteString(`</div>`)
	}

	if strings.TrimSpace(p.Link) != "" {
		if strings.TrimSpace(p.BtnText) != "" {
			// 按钮模式：内容 + 底部按钮。
			ctx.HTML.WriteString(`<div class="`)
			ctx.HTML.WriteString(cls)
			ctx.HTML.WriteString(` wp-infobox">`)
			ctx.HTML.WriteString(body.String())
			ctx.HTML.WriteString(`<a class="wp-infobox-btn" href="`)
			ctx.HTML.WriteString(html.EscapeString(p.Link))
			ctx.HTML.WriteString(`">`)
			ctx.HTML.WriteString(html.EscapeString(p.BtnText))
			ctx.HTML.WriteString(`</a>`)
			ctx.HTML.WriteString(`</div>`)
			compileCSS(node.ID, &p, ctx.CSS)
			return nil
		}
		ctx.HTML.WriteString(`<a class="`)
		ctx.HTML.WriteString(cls)
		ctx.HTML.WriteString(` wp-infobox" href="`)
		ctx.HTML.WriteString(html.EscapeString(p.Link))
		ctx.HTML.WriteString(`">`)
		ctx.HTML.WriteString(body.String())
		ctx.HTML.WriteString(`</a>`)
	} else {
		ctx.HTML.WriteString(`<div class="`)
		ctx.HTML.WriteString(cls)
		ctx.HTML.WriteString(` wp-infobox">`)
		ctx.HTML.WriteString(body.String())
		ctx.HTML.WriteString(`</div>`)
	}

	compileCSS(node.ID, &p, ctx.CSS)
	return nil
}

// compileCSS 信息框样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop []string
	desktop = append(desktop, "display: flex", "flex-direction: column", "gap: 10px")
	align := p.Align
	if align == "" {
		align = "center"
	}
	switch align {
	case "left":
		desktop = append(desktop, "align-items: flex-start", "text-align: left")
	case "right":
		desktop = append(desktop, "align-items: flex-end", "text-align: right")
	default:
		desktop = append(desktop, "align-items: center", "text-align: center")
	}
	if p.Padding != "" {
		desktop = append(desktop, "padding: "+p.Padding)
	}
	if p.Background != "" {
		desktop = append(desktop, "background: "+p.Background)
	}
	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointDesktop, sel+".wp-infobox", []string{
		"text-decoration: none", "color: inherit",
		"transition: transform .18s, box-shadow .18s",
	})
	b.Add(core.BreakpointDesktop, sel+".wp-infobox:hover", []string{
		"transform: translateY(-2px)",
	})

	iconSize := p.IconSize
	if iconSize == "" {
		iconSize = "40px"
	}
	b.Add(core.BreakpointDesktop, sel+" .wp-infobox-icon", []string{
		"width: " + iconSize, "height: " + iconSize,
		"display: inline-flex", "align-items: center", "justify-content: center",
		"font-size: calc(" + iconSize + " * 0.6)",
		"border-radius: 999px",
		"background: rgba(0,0,0,.05)",
	})
	if p.IconColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-icon", []string{"color: " + p.IconColor})
	}
	b.Add(core.BreakpointDesktop, sel+" .wp-infobox-media img", []string{
		"max-width: 100%", "height: auto", "border-radius: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-infobox-title", []string{
		"margin: 0", "font-size: 1.15em", "line-height: 1.3",
	})
	if p.TitleColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-title", []string{"color: " + p.TitleColor})
	}
	b.Add(core.BreakpointDesktop, sel+" .wp-infobox-text", []string{
		"font-size: 0.92em", "line-height: 1.6",
		"opacity: .85",
	})
	if p.TextColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-text", []string{"color: " + p.TextColor, "opacity: 1"})
	}
	if p.Subtitle != "" {
		sub := []string{
			"display: inline-block", "font-size: 0.75em", "font-weight: 600",
			"letter-spacing: 0.06em", "text-transform: uppercase",
			"padding: 3px 10px", "border-radius: 999px",
			"background: rgba(0,0,0,.06)",
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-subtitle", sub)
		if p.SubtitleColor != "" {
			b.Add(core.BreakpointDesktop, sel+" .wp-infobox-subtitle", []string{"color: " + p.SubtitleColor})
		}
	}
	if p.Radius != "" {
		b.Add(core.BreakpointDesktop, sel, []string{"border-radius: " + p.Radius, "overflow: hidden"})
	}
	if p.HoverBg != "" {
		b.Add(core.BreakpointDesktop, sel+".wp-infobox:hover", []string{"background: " + p.HoverBg})
	}
	if p.IconBgColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-icon", []string{"background: " + p.IconBgColor})
	}
	if p.IconBorderColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-icon", []string{"border: 1px solid " + p.IconBorderColor})
	}
	if p.BtnText != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-btn", []string{
			"display: inline-flex", "align-items: center", "justify-content: center",
			"padding: 10px 22px", "border-radius: 999px",
			"background: var(--c-primary, #2563eb)", "color: #fff",
			"text-decoration: none", "font-size: 0.9em", "font-weight: 500",
			"margin-top: 6px", "transition: opacity .15s",
		})
		b.Add(core.BreakpointDesktop, sel+" .wp-infobox-btn:hover", []string{"opacity: .85"})
	}
}
