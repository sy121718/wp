package languages

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"

	"go_wp/internal/builder/core"
)

const Type = "core.languages"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("languages", languagesTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"orientation": "horizontal",
			"gap":         "16px",
		},
		DisplayName: "语言切换",
		Hint:        "多语言站点切换链接",
	}
}

type Props struct {
	Orientation string `json:"orientation,omitempty" ct:"select,horizontal=水平,vertical=垂直,sec=style,label=排列方向"`

	Gap string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`

	ShowCode bool `json:"showCode,omitempty" ct:"bool,sec=content,label=显示语言代码"`

	Color        string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=文字色"`
	HoverColor   string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停文字色"`
	CurrentColor string `json:"currentColor,omitempty" ct:"color,maxlen=200,sec=style,label=当前语言色"`
	FontSize     string `json:"fontSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=字号"`
	FontWeight   string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,sec=style,label=字重"`
	ItemPadding  string `json:"itemPadding,omitempty" ct:"dimension,maxlen=20,sec=style,label=项内距"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	if len(node.Children) > 0 {
		return fmt.Errorf("节点 %s: 语言切换器为原子组件，不允许子节点", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err = core.ValidateAdvanced(core.AdvancedOf(&p), node.ID, ids); err != nil {
		return err
	}
	return core.ValidateSpec(&p, node.ID)
}

//go:embed languages.css
var languagesCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"vertical":      core.BoolVar(p.Orientation == "vertical"),
		"gap":           p.Gap,
		"color":         p.Color,
		"hover_color":   p.HoverColor,
		"current_color": p.CurrentColor,
		"font_size":     p.FontSize,
		"font_weight":   p.FontWeight,
		"item_padding":  p.ItemPadding,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, languagesCSS, vars); err != nil {

		panic(fmt.Sprintf("languages 组件样式解析失败: %v", err))
	}
}

//go:embed languages.jet
var languagesTemplate string
