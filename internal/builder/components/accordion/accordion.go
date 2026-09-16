// Package accordion 实现 core.accordion：手风琴组件（对标 WD wd_accordion）。
// 结构型：children 即各折叠项内容（可嵌套任意组件树）；
// props.items 提供标题列表（与 children 一一对应）。
// 零 JS：原生 <details>/<summary> 展开收起。
package accordion

import (
	_ "embed" // accordion.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.accordion"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("accordion", accordionTemplate)
}

// Component 手风琴组件（结构型）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
func (c *Component) Translatable() []string { return []string{"title"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// Palette 实现 core.PaletteProvider：组件库呈现元数据（审计 REG-005）。
// 显示名 / 说明 / 分组 / 插入默认 Props 都在 Go 侧声明，前端只消费注入数据。
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

// AlignedRepeater 是服务端面板和客户端对齐操作共同消费的唯一声明。
func (c *Component) AlignedRepeater() core.AlignedRepeaterSpec {
	return core.AlignedRepeaterSpec{
		AlignKey: "items", Field: "title", Noun: "折叠项", Label: "标题",
		AddText: "+ 添加折叠项（自动创建内容）",
		Extra:   []core.RepeaterExtra{{Key: "open", Label: "默认展开"}},
	}
}

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
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
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

// accordionCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed accordion.css
var accordionCSS string

// compileCSS 手风琴样式。
//
// Go 侧只把属性翻成变量（描边色、对齐映射、标题字号），无边框模式是一个规则级开关。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 对齐映射：center 保持 center，right 要翻成 flex-end（容器是 flex）。
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

// accordionTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed accordion.jet
var accordionTemplate string
