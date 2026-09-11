// Package tabs 实现 core.tabs：页签组件（对标 WD wd_tabs）。
// 结构型：children 即各页签面板（可嵌套任意组件树）；
// props.tabs 提供标签文案列表（与 children 一一对应）。
// 零 JS 切换：radio + label hack（CSS :checked 控制面板显隐）。
package tabs

import (
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.tabs"

func init() { core.Register(&Component{}) }

// Component 页签组件（结构型）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
func (c *Component) Translatable() []string { return []string{"label"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（样式字段声明式）。
func (c *Component) PropsSpec() any { return &Props{} }

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

// compileCSS 页签样式（radio hack 显隐 + 高亮）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: flex", "flex-direction: column",
	})
	// radio 视觉隐藏但**保留可聚焦**（sr-only）：display:none 会把 tab 从键盘序列里彻底移除，
	// 键盘用户无法切换页签。原生 radio group 自带正确的键盘模型 —— Tab 进组、方向键切换。
	b.Add(core.BreakpointDesktop, sel+" .sky-tabs-radio", []string{
		"position: absolute",
		"width: 1px",
		"height: 1px",
		"margin: -1px",
		"padding: 0",
		"border: 0",
		"clip-path: inset(50%)",
		"overflow: hidden",
		"white-space: nowrap",
	})
	// 聚焦可见：sr-only 的 radio 聚焦时把焦点环画在对应标签上（键盘用户能看见当前位置）。
	b.Add(core.BreakpointDesktop, sel+" .sky-tabs-radio:focus-visible + .sky-tabs-tab", []string{
		"outline: 2px solid var(--sky-c-primary, #2563eb)",
		"outline-offset: 2px",
	})
	// 面板默认隐藏，选中对应 radio 时显示（面板是 radio 的后续兄弟）。
	for i := range p.Tabs {
		radio := "#sky-tabs-" + id + "-" + fmt.Sprintf("%d", i)
		// 面板定位用模板已输出的 data-index（tabs.jet：data-index="{{ i }}"）。
		// 不能用 nth-of-type：它按「元素类型」计数而非按类名，而容器内 .sky-tabs-nav
		// 同样是 div，会占掉 div:nth-of-type(1)，导致所有面板选择器整体错位一位、
		// 匹配不到任何元素 —— 页签切换完全失效（radio 可点但面板永不显示）。
		// 面板选择器**不含 sel 前缀**：:has() 已经挂在容器上，再拼一次会变成
		// `.sel:has(...) .sel .panel`（要求嵌套两层容器）—— 永不匹配、面板永远不显示。
		panel := " .sky-tab-panel[data-index=\"" + fmt.Sprintf("%d", i) + "\"]"
		// 用 :has() 而不是「radio:checked ~ 面板」：radio 现在与标签同处 .sky-tabs-nav，
		// 已不是面板的前兄弟（标签要在同一个容器里，读屏才把单选组读成一组）。
		b.Add(core.BreakpointDesktop, sel+":has("+radio+":checked)"+panel, []string{"display: block"})
	}
	// 标签高亮。
	for i := range p.Tabs {
		radio := "#sky-tabs-" + id + "-" + fmt.Sprintf("%d", i)
		label := " .sky-tabs-nav label:nth-of-type(" + fmt.Sprintf("%d", i+1) + ")"
		b.Add(core.BreakpointDesktop, sel+":has("+radio+":checked)"+label, []string{
			"color: #fff", "background: var(--sky-c-primary, #2563eb)",
		})
	}

	// 基础样式。
	borderColor := p.BorderColor
	if borderColor == "" {
		// 未配置时跟主题边框色（写死的话主题改边框、标签页底边不动）。
		borderColor = "var(--sky-c-border, rgba(0,0,0,.1))"
	}
	navJustify := "flex-start"
	if p.NavAlign == "center" {
		navJustify = "center"
	} else if p.NavAlign == "right" {
		navJustify = "flex-end"
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-tabs-nav", []string{
		"display: flex", "gap: 4px", "flex-wrap: wrap",
		core.CSSDecl("justify-content", navJustify),
		core.CSSDecl("border-bottom", "1px", "solid", borderColor), "padding: 0 4px",
	})
	if p.ActiveColor != "" {
		for i := range p.Tabs {
			radio := "#sky-tabs-" + id + "-" + fmt.Sprintf("%d", i)
			label := sel + " .sky-tabs-nav label:nth-of-type(" + fmt.Sprintf("%d", i+1) + ")"
			b.Add(core.BreakpointDesktop, radio+":checked ~ "+label, []string{
				core.CSSDecl("background", p.ActiveColor),
			})
		}
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-tabs-nav label", []string{
		"padding: 9px 18px", "cursor: pointer", "font-size: 14px",
		"border-radius: 8px 8px 0 0", "transition: background .15s, color .15s",
		"user-select: none",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-tabs-nav label:hover", []string{"background: rgba(0,0,0,.05)"})
	b.Add(core.BreakpointDesktop, sel+" .sky-tab-panel", []string{
		"display: none", "padding: 18px 4px", "animation: sky-tabs-fade .25s ease",
	})
	// 渐显动画。
	b.AddKeyframesDecls("sky-tabs-fade", []string{
		"from { opacity: 0 }", "to { opacity: 1 }",
	})
	// 竖向布局。
	if p.Vertical {
		b.Add(core.BreakpointDesktop, sel+".sky-tabs-vertical", []string{"flex-direction: row", "align-items: stretch"})
		b.Add(core.BreakpointDesktop, sel+".sky-tabs-vertical .sky-tabs-nav", []string{
			"flex-direction: column", "border-bottom: 0", "border-right: 1px solid var(--sky-c-border, rgba(0,0,0,.1))",
			"min-width: 140px", "padding: 4px 0",
		})
		b.Add(core.BreakpointDesktop, sel+".sky-tabs-vertical .sky-tabs-nav label", []string{
			"border-radius: 8px", "margin: 2px 4px",
		})
		b.Add(core.BreakpointDesktop, sel+".sky-tabs-vertical .sky-tab-panel", []string{"flex: 1", "padding: 0 18px"})
	}
}
