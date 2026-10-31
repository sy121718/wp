package counter

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"

	"go_wp/internal/builder/core"
)

const Type = "core.counter"

//go:embed enhance.js
var enhanceJS string

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("counter", counterTemplate)

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCounters"},
		Feats:  []string{"data-counter"},
		Source: enhanceJS,
	})
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) Translatable() []string { return []string{"prefix", "suffix", "label"} }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"start":  0,
			"end":    100,
			"suffix": "+",
		},
		DisplayName: "计数器",
		Hint:        "数字统计",
	}
}

type Props struct {
	Start float64 `json:"start,omitempty" ct:"number,sec=content,label=起始值"`

	End float64 `json:"end,omitempty" ct:"number,sec=content,label=结束值"`

	Decimals int `json:"decimals,omitempty" ct:"int,min=0,max=6,sec=content,label=小数位"`

	Prefix string `json:"prefix,omitempty" ct:"safe,maxlen=20,sec=content,label=前缀"`

	Suffix string `json:"suffix,omitempty" ct:"safe,maxlen=20,sec=content,label=后缀"`

	Label string `json:"label,omitempty" ct:"safe,maxlen=100,sec=content,label=标签"`

	Duration float64 `json:"duration,omitempty" ct:"number,sec=content,label=动画时长(s)"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=数字颜色"`

	FontSize string `json:"fontSize,omitempty" ct:"dimension,maxlen=30,sec=style,label=字号"`

	Align string `json:"align,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=center,sec=style,label=对齐"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

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

func formatInt(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}

//go:embed counter.css
var counterCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	align := p.Align
	if align == "" {
		align = "center"
	}
	fontSize := p.FontSize
	if fontSize == "" {
		fontSize = "2rem"
	}
	color := p.Color
	if color == "" {
		color = "inherit"
	}
	dur := p.Duration
	if dur <= 0 {
		dur = 2
	}

	vars := map[string]string{
		"align":      align,
		"fontSize":   fontSize,
		"color":      color,
		"cssCounter": core.BoolVar(p.Decimals == 0),
		"from":       formatInt(p.Start),
		"to":         formatInt(p.End),
		"duration":   strconv.FormatFloat(dur, 'f', -1, 64) + "s",
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, counterCSS, vars); err != nil {
		panic(fmt.Sprintf("counter 组件样式解析失败: %v", err))
	}
}

//go:embed counter.jet
var counterTemplate string
