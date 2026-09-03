// Package accordion 实现 core.accordion：手风琴组件（对标 WD wd_accordion）。
// 结构型：children 即各折叠项内容（可嵌套任意组件树）；
// props.items 提供标题列表（与 children 一一对应）。
// 零 JS：原生 <details>/<summary> 展开收起。
package accordion

import (
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.accordion"

func init() { core.Register(&Component{}) }

// Component 手风琴组件（结构型）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Item 折叠项。
type Item struct {
	// Title 标题。
	Title string `json:"title,omitempty"`
	// Open 默认展开。
	Open bool `json:"open,omitempty"`
}

// Props 手风琴属性。
type Props struct {
	// Items 折叠项标题列表（与 children 一一对应）。
	Items []Item `json:"items,omitempty"`
	// OneOpen 同一时间只允许一个展开（手风琴严格模式）。
	OneOpen bool `json:"oneOpen,omitempty" ct:"bool,sec=style,label=同时只开一个"`
	// Borderless 无边框样式（简单分隔线）。
	Borderless bool `json:"borderless,omitempty" ct:"bool,sec=style,label=无边框"`
	// TitleAlign 标题对齐：left / center / right。
	TitleAlign string `json:"titleAlign,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=标题对齐"`
	// BgColor 项背景色。
	BgColor string `json:"bgColor,omitempty" ct:"color,maxlen=200,sec=style,label=项背景色"`
	// TitleSize 标题字号。
	TitleSize string `json:"titleSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=标题字号"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Validate 校验：标题数需与 children 一致且至少一个。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) == 0 {
		return fmt.Errorf("节点 %s: 手风琴至少需要一个折叠项（把组件拖入内部）", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if len(p.Items) != len(node.Children) {
		return fmt.Errorf("节点 %s: 折叠项标题数（%d）需与内容数（%d）一致", node.ID, len(p.Items), len(node.Children))
	}
	for i, it := range p.Items {
		if it.Title == "" {
			return fmt.Errorf("节点 %s: 第 %d 个折叠项缺少标题", node.ID, i+1)
		}
	}
	for _, child := range node.Children {
		if err = core.ValidateNode(child, ids); err != nil {
			return fmt.Errorf("节点 %s 子节点: %w", node.ID, err)
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

// compileCSS 手风琴样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{"display: flex", "flex-direction: column", "gap: 8px"})

	head := sel + " .wp-accordion-head"
	headRules := []string{
		"list-style: none", "cursor: pointer", "user-select: none",
		"display: flex", "align-items: center", "justify-content: space-between",
		"padding: 14px 18px", "font-size: 15px", "font-weight: 600",
		"background: var(--c-surface, #fff)",
		"border: 1px solid rgba(0,0,0,.1)", "border-radius: 10px",
	}
	if p.BgColor != "" {
		headRules = append(headRules, core.CSSDecl("background", p.BgColor))
	}
	if p.TitleAlign == "center" || p.TitleAlign == "right" {
		headRules = append(headRules, core.CSSDecl("justify-content", map[string]string{"center": "center", "right": "flex-end"}[p.TitleAlign]))
	}
	if p.TitleSize != "" {
		headRules = append(headRules, core.CSSDecl("font-size", p.TitleSize))
	}
	b.Add(core.BreakpointDesktop, head, headRules)
	// 展开箭头（summary 伪元素）。
	b.Add(core.BreakpointDesktop, head+"::-webkit-details-marker", []string{"display: none"})
	b.Add(core.BreakpointDesktop, head+"::after", []string{
		"content: '＋'", "font-size: 14px", "color: rgba(0,0,0,.4)",
		"transition: transform .2s", "margin-left: 12px",
	})
	b.Add(core.BreakpointDesktop, sel+" details[open] "+head+"::after", []string{"transform: rotate(45deg)"})
	b.Add(core.BreakpointDesktop, head+":hover", []string{"background: var(--c-bg-hover, #f3f4f6)"})

	b.Add(core.BreakpointDesktop, sel+" .wp-accordion-body", []string{
		"padding: 14px 18px", "border: 1px solid rgba(0,0,0,.08)",
		"border-top: 0", "border-radius: 0 0 10px 10px",
		"margin-top: -8px",
	})

	// 无边框模式。
	if p.Borderless {
		b.Add(core.BreakpointDesktop, sel+".wp-accordion-borderless", []string{"gap: 0"})
		b.Add(core.BreakpointDesktop, sel+".wp-accordion-borderless .wp-accordion-head", []string{
			"border: 0", "border-bottom: 1px solid rgba(0,0,0,.1)", "border-radius: 0",
			"padding-left: 0", "padding-right: 0",
		})
		b.Add(core.BreakpointDesktop, sel+".wp-accordion-borderless .wp-accordion-body", []string{
			"border: 0", "border-radius: 0", "padding-left: 0", "padding-right: 0",
		})
	}
}
