// Package progress 实现 core.progress 进度条组件（对标 GrapesJS progress 组件生态）。
//
// 轻量数据展示原子组件：值/上限 + 可选标签，输出语义化 role=progressbar 结构
// （aria-valuenow/valuemax），进度宽度由 compileCSS 按 value/max 百分比生成，零客户端 JS。
// validateExtra 强约束 value ∈ [0, max]（max 缺省 100）。
package progress

import (
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.progress"

// defaultMax 缺省进度上限。
const defaultMax = 100

// Props progress 属性。
type Props struct {
	// Value 当前进度值（0~Max）。
	Value int `json:"value,omitempty" ct:"int,min=0,max=100,sec=content,label=当前值"`
	// Max 进度上限（默认 100）。
	Max int `json:"max,omitempty" ct:"int,min=1,max=100,sec=content,label=最大值"`
	// Label 标签（进度说明文字）。
	Label string `json:"label,omitempty" ct:"text,maxlen=100,sec=content,label=标签"`
	// Color 进度填充色（色值或主题 Token）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：值非负且不超过上限。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Max < 0 {
		return fmt.Errorf("进度上限不能为负: %d", p.Max)
	}
	if p.Value < 0 {
		return fmt.Errorf("进度值不能为负: %d", p.Value)
	}
	max := effectiveMax(p)
	if p.Value > max {
		return fmt.Errorf("进度值 %d 超过上限 %d", p.Value, max)
	}
	return nil
}

// effectiveMax 有效上限（0 视为缺省 100）。
func effectiveMax(p *Props) int {
	if p.Max <= 0 {
		return defaultMax
	}
	return p.Max
}

// percent 进度百分比宽度（0~100，整数截断，确定性）。
func percent(p *Props) int {
	max := effectiveMax(p)
	v := p.Value
	if v < 0 {
		v = 0
	}
	if v > max {
		v = max
	}
	if max <= 0 {
		return 0
	}
	return v * 100 / max
}

// compileCSS 进度条轨道/填充/标签样式（宽度由百分比生成）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	color := p.Color
	if color == "" {
		color = "var(--wp-c-primary, #2563eb)"
	}
	pct := percent(p)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex",
		"align-items: center",
		"gap: 10px",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-progress-track", []string{
		"flex: 1",
		"height: 8px",
		"background: rgba(0,0,0,0.08)",
		"border-radius: 9999px",
		"overflow: hidden",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-progress-bar", []string{
		core.CSSDecl("width", fmt.Sprintf("%d%%", pct)),
		core.CSSDecl("background", color),
		"height: 100%",
		"border-radius: inherit",
	})
	b.Add(core.BreakpointDesktop, sel+" .wp-progress-label", []string{
		"font-size: 0.875rem",
		"opacity: 0.8",
	})
}

// init 注册进度条组件。
func init() {
	core.Register(Widget)
}
