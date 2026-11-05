package accordion

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"

	"go_wp/pkg/builder/core"
)

const Type = "core.accordion"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("accordion", accordionTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) Translatable() []string { return []string{"title"} }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"items": []any{
				map[string]any{
					"title": "折叠项一",
					"open":  true,
				},
			},
		},
		DisplayName: "手风琴",
		Hint:        "折叠展开",
	}
}

func (c *Component) AlignedRepeater() core.AlignedRepeaterSpec {
	return core.AlignedRepeaterSpec{
		AlignKey: "items", Field: "title", Noun: "折叠项", Label: "标题",
		AddText: "+ 添加折叠项（自动创建内容）",
		Extra:   []core.RepeaterExtra{{Key: "open", Label: "默认展开"}},
	}
}

type Item struct {
	Title string `json:"title,omitempty"`

	Open bool `json:"open,omitempty"`
}

type Props struct {
	Items []Item `json:"items,omitempty"`

	OneOpen bool `json:"oneOpen,omitempty" ct:"bool,sec=style,label=同时只开一个"`

	Borderless bool `json:"borderless,omitempty" ct:"bool,sec=style,label=无边框"`

	TitleAlign string `json:"titleAlign,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=标题对齐"`

	BgColor string `json:"bgColor,omitempty" ct:"color,maxlen=200,sec=style,label=项背景色"`

	TitleSize string `json:"titleSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=标题字号"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) error {
	if err := c.ValidateSelf(node, ids); err != nil {
		return err
	}
	return core.ValidateChildren(node, ids)
}

func (c *Component) ValidateSelf(node *core.Node, ids map[string]bool) (err error) {
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
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	return nil
}

//go:embed accordion.css
var accordionCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	justify := ""
	switch p.TitleAlign {
	case "center":
		justify = "center"
	case "right":
		justify = "flex-end"
	}

	vars := map[string]string{
		"bgColor":    p.BgColor,
		"justify":    justify,
		"titleSize":  p.TitleSize,
		"borderless": core.BoolVar(p.Borderless),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, accordionCSS, vars); err != nil {
		panic(fmt.Sprintf("accordion 组件样式解析失败: %v", err))
	}
}

//go:embed accordion.jet
var accordionTemplate string
