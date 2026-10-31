package tabs

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"

	"go_wp/internal/builder/core"
)

const Type = "core.tabs"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("tabs", tabsTemplate)
}

type Component struct{}

func (c *Component) Type() string { return Type }

func (c *Component) Translatable() []string { return []string{"label"} }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"tabs": []any{
				map[string]any{
					"label": "页签一",
				},
			},
		},
		DisplayName: "页签",
		Hint:        "多面板切换",
	}
}

func (c *Component) AlignedRepeater() core.AlignedRepeaterSpec {
	return core.AlignedRepeaterSpec{
		AlignKey: "tabs", Field: "label", Noun: "页签", Label: "标签",
		AddText: "+ 添加页签（自动创建面板）",
	}
}

type Tab struct {
	Label string `json:"label,omitempty"`

	Icon string `json:"icon,omitempty"`
}

type Props struct {
	Tabs []Tab `json:"tabs,omitempty"`

	Vertical bool `json:"vertical,omitempty" ct:"bool,sec=style,label=竖向页签"`

	NavAlign string `json:"navAlign,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=标签对齐"`

	ActiveColor string `json:"activeColor,omitempty" ct:"color,maxlen=200,sec=style,label=激活页签背景色"`

	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=200,sec=style,label=导航边框色"`

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
		return fmt.Errorf("节点 %s: 页签至少需要一个面板（把组件拖入内部）", node.ID)
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if len(p.Tabs) != len(node.Children) {
		return fmt.Errorf("节点 %s: 页签数量（%d）需与面板数量（%d）一致", node.ID, len(p.Tabs), len(node.Children))
	}
	for i, t := range p.Tabs {
		if t.Label == "" {
			return fmt.Errorf("节点 %s: 第 %d 个页签缺少标签", node.ID, i+1)
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

//go:embed tabs.css
var tabsCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	tabs := make([]map[string]string, 0, len(p.Tabs))
	for i := range p.Tabs {
		n := strconv.Itoa(i)
		tabs = append(tabs, map[string]string{
			"radio": "#sky-tabs-" + id + "-" + n,
			"index": n,
			"nth":   strconv.Itoa(i + 1),
		})
	}

	borderColor := p.BorderColor
	if borderColor == "" {
		borderColor = "var(--sky-c-border, rgba(0,0,0,.1))"
	}
	navJustify := "flex-start"
	switch p.NavAlign {
	case "center":
		navJustify = "center"
	case "right":
		navJustify = "flex-end"
	}

	vars := map[string]string{
		"navJustify":  navJustify,
		"borderColor": borderColor,
		"activeColor": p.ActiveColor,
		"vertical":    core.BoolVar(p.Vertical),
	}
	lists := map[string][]map[string]string{"tabs": tabs}
	if err := core.ApplyComponentCSSTmplLists(b, sel, tabsCSS, vars, lists); err != nil {
		panic(fmt.Sprintf("tabs 组件样式解析失败: %v", err))
	}
}

//go:embed tabs.jet
var tabsTemplate string
