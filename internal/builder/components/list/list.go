// Package list 实现 core.list：列表组件（对标 WD wd_list）。
// 支持三种样式：自定义图标 / 序号 / 圆点；每项可带链接。
package list

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.list"

func init() { core.Register(&Component{}) }

// Component 列表组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Item 列表项。
type Item struct {
	// Icon 自定义图标名（内置图标集：check/star/arrow-right/cross 等；空则用样式默认）。
	Icon string `json:"icon,omitempty"`
	// Text 文本内容。
	Text string `json:"text,omitempty"`
	// Link 可选项链接。
	Link string `json:"link,omitempty"`
}

// Style 列表样式：icon 自定义图标 / number 序号 / dot 圆点。
type Style string

const (
	StyleIcon   Style = "icon"
	StyleNumber Style = "number"
	StyleDot    Style = "dot"
)

// Props 列表属性。
type Props struct {
	// Items 列表项（repeater）。
	Items []Item `json:"items,omitempty"`
	// Style 列表样式：icon / number / dot。
	Style Style `json:"style,omitempty" ct:"select,icon=图标,number=序号,dot=圆点,default=icon,sec=content,label=列表样式"`
	// IconColor 图标颜色（样式为 icon 时）。
	IconColor string `json:"iconColor,omitempty" ct:"safe,maxlen=200,sec=style,label=图标颜色"`
	// IconSize 图标尺寸（px，样式为 icon 时）。
	IconSize string `json:"iconSize,omitempty" ct:"safe,maxlen=20,sec=style,label=图标尺寸"`
	// TextColor 文本颜色。
	TextColor string `json:"textColor,omitempty" ct:"safe,maxlen=200,sec=style,label=文本颜色"`
	// Spacing 项间距（px）。
	Spacing string `json:"spacing,omitempty" ct:"safe,maxlen=20,sec=style,label=项间距"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// builtinIcons 内置图标（内联 SVG path，fill=currentColor）。
var builtinIcons = map[string]string{
	"check":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" width="1em" height="1em"><polyline points="20 6 9 17 4 12"/></svg>`,
	"star":   `<svg viewBox="0 0 24 24" fill="currentColor" width="1em" height="1em"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>`,
	"arrow":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" width="1em" height="1em"><line x1="5" y1="12" x2="19" y2="12"/><polyline points="12 5 19 12 12 19"/></svg>`,
	"cross":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" width="1em" height="1em"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`,
	"shield": `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="1em" height="1em"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg>`,
	"truck":  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="1em" height="1em"><rect x="1" y="3" width="15" height="13" rx="1"/><path d="M16 8h4l3 3v5h-7V8z"/><circle cx="5.5" cy="18.5" r="2.5"/><circle cx="18.5" cy="18.5" r="2.5"/></svg>`,
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 列表为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	switch p.Style {
	case "", StyleIcon, StyleNumber, StyleDot:
	default:
		return fmt.Errorf("节点 %s: 无效的列表样式 %q", node.ID, p.Style)
	}
	if p.Style == StyleIcon {
		for _, it := range p.Items {
			if it.Icon != "" {
				if _, ok := builtinIcons[it.Icon]; !ok {
					return fmt.Errorf("节点 %s: 未知图标 %q", node.ID, it.Icon)
				}
			}
		}
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

// Render 渲染列表。
func (c *Component) Render(node *core.Node, topLevel bool, ctx *core.RenderContext) (err error) {
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	style := p.Style
	if style == "" {
		style = StyleIcon
	}
	cls := core.NodeClass(node.ID)

	ctx.HTML.WriteString(`<ul class="`)
	ctx.HTML.WriteString(cls)
	ctx.HTML.WriteString(` wp-list wp-list-`)
	ctx.HTML.WriteString(string(style))
	ctx.HTML.WriteString(`">`)
	for i, item := range p.Items {
		ctx.HTML.WriteString(`<li class="wp-list-item">`)
		// 前缀：图标 / 序号 / 圆点。
		ctx.HTML.WriteString(`<span class="wp-list-marker" aria-hidden="true">`)
		switch style {
		case StyleIcon:
			if svg, ok := builtinIcons[item.Icon]; ok {
				ctx.HTML.WriteString(svg)
			} else if item.Icon != "" {
				ctx.HTML.WriteString(html.EscapeString(item.Icon))
			} else {
				ctx.HTML.WriteString(builtinIcons["check"])
			}
		case StyleNumber:
			ctx.HTML.WriteString(fmt.Sprintf("%d", i+1))
		default: // dot
			ctx.HTML.WriteString(`<i class="wp-list-dot"></i>`)
		}
		ctx.HTML.WriteString(`</span>`)
		// 文本（可链接）。
		if strings.TrimSpace(item.Link) != "" {
			ctx.HTML.WriteString(`<a class="wp-list-text" href="`)
			ctx.HTML.WriteString(html.EscapeString(item.Link))
			ctx.HTML.WriteString(`">`)
			ctx.HTML.WriteString(html.EscapeString(item.Text))
			ctx.HTML.WriteString(`</a>`)
		} else {
			ctx.HTML.WriteString(`<span class="wp-list-text">`)
			ctx.HTML.WriteString(html.EscapeString(item.Text))
			ctx.HTML.WriteString(`</span>`)
		}
		ctx.HTML.WriteString(`</li>`)
	}
	ctx.HTML.WriteString(`</ul>`)

	compileCSS(node.ID, &p, ctx.CSS)
	return nil
}

// compileCSS 列表样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop []string
	desktop = append(desktop, "list-style: none")
	desktop = append(desktop, "margin: 0")
	desktop = append(desktop, "padding: 0")

	spacing := p.Spacing
	if spacing == "" {
		spacing = "10px"
	}
	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointDesktop, sel+" .wp-list-item", []string{
		"display: flex", "align-items: flex-start", "gap: 10px",
		"padding: calc(" + spacing + " / 2) 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-list-marker", []string{
		"flex: none", "display: inline-flex", "align-items: center",
		"justify-content: center", "width: 1.3em", "height: 1.3em",
		"margin-top: 2px",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-list-marker svg", []string{
		"width: 1em", "height: 1em", "display: block",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-list-text", []string{
		"flex: 1", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" a.wp-list-text", []string{
		"text-decoration: none", "color: inherit",
	})
	b.Add(core.BreakpointDesktop, sel+" a.wp-list-text:hover", []string{
		"text-decoration: underline",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-list-dot", []string{
		"width: 8px", "height: 8px", "border-radius: 999px",
		"background: currentColor", "display: block", "margin-top: 6px",
	})

	if p.IconColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-marker", []string{"color: " + p.IconColor})
	}
	if p.TextColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-text", []string{"color: " + p.TextColor})
	}
	if p.IconSize != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-marker", []string{"font-size: " + p.IconSize})
	}
}
