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

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
func (c *Component) Translatable() []string { return []string{"prefix", "suffix", "label"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Props 计数器属性。
type Props struct {
	// Start 起始值。
	Start float64 `json:"start,omitempty" ct:"number,sec=content,label=起始值"`
	// End 结束值（目标）。
	End float64 `json:"end,omitempty" ct:"number,sec=content,label=结束值"`
	// Decimals 小数位（默认 0）。
	Decimals int `json:"decimals,omitempty" ct:"int,min=0,max=6,sec=content,label=小数位"`
	// Prefix 前缀（如 $ / + / 已售）。
	Prefix string `json:"prefix,omitempty" ct:"safe,maxlen=20,sec=content,label=前缀"`
	// Suffix 后缀（如 % / + / 万）。
	Suffix string `json:"suffix,omitempty" ct:"safe,maxlen=20,sec=content,label=后缀"`
	// Label 底部标签（如「满意客户」）。
	Label string `json:"label,omitempty" ct:"safe,maxlen=100,sec=content,label=标签"`
	// Duration 动画时长（秒，默认 2）。
	Duration float64 `json:"duration,omitempty" ct:"number,sec=content,label=动画时长(s)"`
	// Color 数字颜色。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=数字颜色"`
	// FontSize 数字字号。
	FontSize string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style,label=字号"`
	// Align 对齐：left / center / right。
	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
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

// formatInt 整数展示（CSS 计数模式：counter-reset 只接受整数）。
func formatInt(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
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
		"display: flex", "align-items: baseline", core.CSSDecl("justify-content", align),
		"gap: 4px", core.CSSDecl("text-align", textAlign),
	}
	if p.FontSize != "" {
		desktop = append(desktop, core.CSSDecl("font-size", p.FontSize))
	} else {
		desktop = append(desktop, "font-size: 2rem")
	}
	if p.Color != "" {
		desktop = append(desktop, core.CSSDecl("color", p.Color))
	} else {
		desktop = append(desktop, "color: inherit")
	}
	desktop = append(desktop, "font-weight: 700", "line-height: 1.2")
	b.Add(core.BreakpointDesktop, sel, desktop)

	b.Add(core.BreakpointDesktop, sel+" .sky-counter-value", []string{"font-variant-numeric: tabular-nums"})
	// 整数模式零 JS 计数（@property 注册 <integer> 自定义属性 + counter() 显示）：
	// 动画由 view() 时间线驱动（滚动进入视口计数），老浏览器忽略 timeline 后
	// 动画立即完成显示终值（优雅降级）。小数位模式仍走内嵌脚本（CSS counter 只支持整数）。
	if p.Decimals == 0 {
		dur := p.Duration
		if dur <= 0 {
			dur = 2
		}
		b.Add(core.BreakpointDesktop, sel, []string{
			core.CSSDecl("--sky-counter-from", formatInt(p.Start)),
			core.CSSDecl("--sky-counter-to", formatInt(p.End)),
			core.CSSDecl("--sky-counter-duration", strconv.FormatFloat(dur, 'f', -1, 64)+"s"),
			"counter-reset: wpcount var(--sky-count)",
			"animation: sky-counter-run var(--sky-counter-duration) linear both",
			"animation-timeline: view()",
			"animation-range: entry 0% entry 80%",
		})
		b.Add(core.BreakpointDesktop, sel+" .sky-counter-value::after", []string{"content: counter(wpcount)"})
		// 共享资源（AddKeyframes 按名去重：多计数器只输出一份）。
		b.AddKeyframes("sky-counter-property", "@property --sky-count {\n  syntax: \"<integer>\"\n  initial-value: 0\n  inherits: false\n}")
		b.AddKeyframesDecls("sky-counter-run", []string{
			"from { --sky-count: var(--sky-counter-from) }",
			"to { --sky-count: var(--sky-counter-to) }",
		})
	}
	b.Add(core.BreakpointDesktop, "div"+sel+"-label.sky-counter-label, .sky-counter-label", []string{
		"font-size: 0.85rem", "font-weight: 400", "opacity: .7",
		"margin-top: 6px", core.CSSDecl("text-align", textAlign),
	})
}
