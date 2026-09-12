// Package tabs 实现 core.tabs：页签组件（对标 WD wd_tabs）。
// 结构型：children 即各页签面板（可嵌套任意组件树）；
// props.tabs 提供标签文案列表（与 children 一一对应）。
// 零 JS 切换：radio + label hack（CSS :checked 控制面板显隐）。
package tabs

import (
	_ "embed" // tabs.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.tabs"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("tabs", tabsTemplate)
}

// Component 页签组件（结构型）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
func (c *Component) Translatable() []string { return []string{"label"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

// AlignedRepeater 是服务端面板和客户端对齐操作共同消费的唯一声明。
func (c *Component) AlignedRepeater() core.AlignedRepeaterSpec {
	return core.AlignedRepeaterSpec{
		AlignKey: "tabs", Field: "label", Noun: "页签", Label: "标签",
		AddText: "+ 添加页签（自动创建面板）",
	}
}

// Tab 页签标签项。
type Tab struct {
	// Label 标签文案。
	Label string `json:"label,omitempty"`
	// Icon 可选图标名（内置图标集与 list 一致）。
	Icon string `json:"icon,omitempty"`
}

// Props 页签属性。
type Props struct {
	// Tabs 标签列表（与 children 面板一一对应）。
	Tabs []Tab `json:"tabs,omitempty"`
	// Vertical 竖向布局（标签在左）。
	Vertical bool `json:"vertical,omitempty" ct:"bool,sec=style,label=竖向页签"`
	// NavAlign 标签对齐：left / center / right。
	NavAlign string `json:"navAlign,omitempty" ct:"select,left=左对齐,center=居中,right=右对齐,default=left,sec=style,label=标签对齐"`
	// ActiveColor 激活页签背景色。
	ActiveColor string `json:"activeColor,omitempty" ct:"color,maxlen=200,sec=style,label=激活页签背景色"`
	// BorderColor 导航底边框色。
	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=200,sec=style,label=导航边框色"`
	// Advanced 通用高级属性。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Validate 校验：标签数需与面板数一致且至少一个。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
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

// tabsCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed tabs.css
var tabsCSS string

// compileCSS 页签样式（radio hack 显隐 + 高亮）。
//
// 每个页签的一组规则交给样式源的 @each 展开：规则数量随标签数变化，值变量表达不了。
// Go 侧只负责把「第 i 个页签的 radio id / 面板下标 / 标签序号」算出来，
// 以及对齐映射与两个色值的默认值。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 页签列表：radio 的 id 与面板下标共用同一个实例内序号。
	tabs := make([]map[string]string, 0, len(p.Tabs))
	for i := range p.Tabs {
		n := strconv.Itoa(i)
		tabs = append(tabs, map[string]string{
			"radio": "#sky-tabs-" + id + "-" + n,
			"index": n,
			"nth":   strconv.Itoa(i + 1),
		})
	}

	// 未配置时跟主题边框色（写死的话主题改边框、页签底边不动）。
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

// tabsTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed tabs.jet
var tabsTemplate string
