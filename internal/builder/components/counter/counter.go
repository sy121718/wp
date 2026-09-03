// Package counter 实现 core.counter：数字计数器（对标 WD wd_counter）。
// 起始/结束值 + 前缀/后缀 + 小数位；轻量增强：滚动到视口时数字递增动画。
package counter

import (
	"encoding/json"
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.counter"

func init() { core.Register(&Component{}) }

// Component 计数器组件（原子）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Props 计数器属性。
type Props struct {
	// Start 起始值。
	Start float64 `json:"start,omitempty"`
	// End 结束值（目标）。
	End float64 `json:"end,omitempty"`
	// Decimals 小数位（默认 0）。
	Decimals int `json:"decimals,omitempty"`
	// Prefix 前缀（如 $ / + / 已售）。
	Prefix string `json:"prefix,omitempty" ct:"safe,maxlen=20,sec=content,label=前缀"`
	// Suffix 后缀（如 % / + / 万）。
	Suffix string `json:"suffix,omitempty" ct:"safe,maxlen=20,sec=content,label=后缀"`
	// Label 底部标签（如「满意客户」）。
	Label string `json:"label,omitempty" ct:"safe,maxlen=100,sec=content,label=标签"`
	// Duration 动画时长（秒，默认 2）。
	Duration float64 `json:"duration,omitempty"`
	// Color 数字颜色。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=数字颜色"`
	// FontSize 数字字号。
	FontSize string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style,label=字号"`
	// Align 对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Validate 校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 计数器为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if p.Decimals < 0 || p.Decimals > 6 {
		return fmt.Errorf("节点 %s: 小数位需在 0~6 之间", node.ID)
	}
	switch p.Align {
	case "", "left", "center", "right":
	default:
		return fmt.Errorf("节点 %s: 无效的对齐 %q", node.ID, p.Align)
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
		if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
return nil
}

func formatNum(v float64, decimals int) string {
	return strconv.FormatFloat(v, 'f', decimals, 64)
}

// compileCSS 计数器样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	align := p.Align
	if align == "" {
		align = "center"
	}
	textAlign := align

	desktop := []string{
		"display: flex", "align-items: baseline", "justify-content: " + align,
		"gap: 4px", "text-align: " + textAlign,
	}
	if p.FontSize != "" {
		desktop = append(desktop, "font-size: "+p.FontSize)
	} else {
		desktop = append(desktop, "font-size: 2rem")
	}
	if p.Color != "" {
		desktop = append(desktop, "color: "+p.Color)
	} else {
		desktop = append(desktop, "color: inherit")
	}
	desktop = append(desktop, "font-weight: 700", "line-height: 1.2")
	b.Add(core.BreakpointDesktop, sel, desktop)

	b.Add(core.BreakpointDesktop, sel+" .wp-counter-value", []string{"font-variant-numeric: tabular-nums"})
	b.Add(core.BreakpointDesktop, "div"+sel+"-label.wp-counter-label, .wp-counter-label", []string{
		"font-size: 0.85rem", "font-weight: 400", "opacity: .7",
		"margin-top: 6px", "text-align: " + textAlign,
	})
}

