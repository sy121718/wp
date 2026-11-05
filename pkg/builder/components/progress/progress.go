package progress

import (
	_ "embed"
	"fmt"

	"go_wp/pkg/builder/core"
)

const Type = "core.progress"

const defaultMax = 100

type Props struct {
	Value int `json:"value,omitempty" ct:"int,min=0,max=100,sec=content,label=当前值"`

	Max int `json:"max,omitempty" ct:"int,min=1,max=100,sec=content,label=最大值"`

	Label string `json:"label,omitempty" ct:"text,maxlen=100,sec=content,label=标签"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "进度条",
		Hint:            "数据进度",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"value": 60,
			"max":   100,
			"label": "完成度",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"label"},
	},
}

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

func effectiveMax(p *Props) int {
	if p.Max <= 0 {
		return defaultMax
	}
	return p.Max
}

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

//go:embed progress.css
var progressCSS string

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

		panic(fmt.Sprintf("progress 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("progress", progressTemplate)
}

//go:embed progress.jet
var progressTemplate string
