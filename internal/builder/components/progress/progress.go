// Package progress 实现 core.progress 进度条组件（对标 GrapesJS progress 组件生态）。
//
// 轻量数据展示原子组件：值/上限 + 可选标签，输出语义化 role=progressbar 结构
// （aria-valuenow/valuemax），进度宽度由 compileCSS 按 value/max 百分比生成，零客户端 JS。
// validateExtra 强约束 value ∈ [0, max]（max 缺省 100）。
package progress

import (
	_ "embed" // progress.css 经 //go:embed 打进二进制
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
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"label"},
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

// progressCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed progress.css
var progressCSS string

// compileCSS 进度条轨道/填充/标签样式（宽度由百分比生成，颜色可为色值或主题 Token）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	color := p.Color
	if color == "" {
		color = "var(--sky-c-primary, #2563eb)"
	}
	vars := map[string]string{
		"width": fmt.Sprintf("%d%%", percent(p)),
		"color": color,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, progressCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("progress 组件样式解析失败: %v", err))
	}
}

// init 注册进度条组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("progress", progressTemplate)
}

// progressTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed progress.jet
var progressTemplate string
