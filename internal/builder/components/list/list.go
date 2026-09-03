// Package list 实现 core.list：列表组件（对标 WD wd_list）。
// 支持三种样式：自定义图标 / 序号 / 圆点；每项可带链接。
package list

import (
	"encoding/json"
	"fmt"
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

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

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
	IconColor string `json:"iconColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标颜色"`
	// IconSize 图标尺寸（px，样式为 icon 时）。
	IconSize string `json:"iconSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=图标尺寸"`
	// TextColor 文本颜色。
	TextColor string `json:"textColor,omitempty" ct:"color,maxlen=200,sec=style,label=文本颜色"`
	// TextSize 文本字号。
	TextSize string `json:"textSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=文本字号"`
	// LinkColor 链接颜色。
	LinkColor string `json:"linkColor,omitempty" ct:"color,maxlen=200,sec=style,label=链接颜色"`
	// LinkColorHover 链接悬停颜色。
	LinkColorHover string `json:"linkColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=链接悬停颜色"`
	// IconBgColor 图标背景色（圆形底）。
	IconBgColor string `json:"iconBgColor,omitempty" ct:"color,maxlen=200,sec=style,label=图标背景色"`
	// IconBgColorHover 图标悬停背景色。
	IconBgColorHover string `json:"iconBgColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=图标悬停背景色"`
	// IconColorHover 图标悬停颜色。
	IconColorHover string `json:"iconColorHover,omitempty" ct:"color,maxlen=200,sec=style,label=图标悬停颜色"`
	// Align 对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=对齐"`
	// Spacing 项间距（px）。
	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
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
				if _, ok := core.IconSVG(it.Icon); !ok {
					return fmt.Errorf("节点 %s: 未知图标 %q", node.ID, it.Icon)
				}
			}
		}
	}
	for i, it := range p.Items {
		if link := strings.TrimSpace(it.Link); link != "" && !core.IsSafeURL(link) {
			return fmt.Errorf("节点 %s: 第 %d 项链接含危险协议: %q", node.ID, i+1, it.Link)
		}
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
		if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
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
	if p.IconBgColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-marker", []string{
			"background: " + p.IconBgColor, "border-radius: 999px",
			"width: 1.8em", "height: 1.8em",
		})
	}
	if p.IconColorHover != "" || p.IconBgColorHover != "" {
		var hv []string
		if p.IconColorHover != "" {
			hv = append(hv, "color: "+p.IconColorHover)
		}
		if p.IconBgColorHover != "" {
			hv = append(hv, "background: "+p.IconBgColorHover)
		}
		b.Add(core.BreakpointDesktop, sel+" .wp-list-item:hover .wp-list-marker", hv)
	}
	if p.TextColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-text", []string{"color: " + p.TextColor})
	}
	if p.TextSize != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-text", []string{"font-size: " + p.TextSize})
	}
	if p.LinkColor != "" {
		b.Add(core.BreakpointDesktop, sel+" a.wp-list-text", []string{"color: " + p.LinkColor})
	}
	if p.LinkColorHover != "" {
		b.Add(core.BreakpointDesktop, sel+" a.wp-list-text:hover", []string{"color: " + p.LinkColorHover})
	}
	if p.IconSize != "" {
		b.Add(core.BreakpointDesktop, sel+" .wp-list-marker", []string{"font-size: " + p.IconSize})
	}
	if p.Align == "center" || p.Align == "right" {
		j := "flex-start"
		if p.Align == "center" {
			j = "center"
		} else {
			j = "flex-end"
		}
		b.Add(core.BreakpointDesktop, sel, []string{"align-items: " + j})
	}
}
