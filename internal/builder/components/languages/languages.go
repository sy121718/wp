// Package languages 实现 core.languages：站点语言切换器（多语言 P3/P4，docs/06-D）。
//
// 访问面硬约束（docs/06-D §2）：零 JS —— 切换器只输出纯链接（<a hreflang>），
// 不写跳转脚本、不写 Cookie 协商。链接清单由构建期注入（core.RenderContext.Locales，
// 装配层按「本页逻辑路径 + 站点启用语言」逐语言算出），因此产物是纯静态的。
//
// 形态选择（为什么是独立组件，而不是 document.jet 全局注入 / nav 的一个选项）：
//  1. 访问面所有可见 UI 都由组件产生，document.jet 只是骨架（head + body 包裹），
//     把可见结构塞进骨架会打破「body 内容 = 文档编译产物」的语义，且骨架没有
//     组件 CSS 通道（样式只会进 .CSS 桶，骨架无法表达位置与可见性开关）；
//  2. 导航（core.nav）的职责是站点菜单，语言切换不是菜单项，混入会让 navigation
//     模块承担非菜单职责，且只在「放了导航的页面」生效；
//  3. 独立组件可放进页眉全局块（block 模块 global 引用）→ 全站一次放置即生效，
//     这是站点级复用的既有机制；
//  4. 组件是「同一文档、多语言各自一份产物」的自然受益者：文档只维护一份，
//     构建期按当前语言渲染当前项标记与各语言链接。
//
// 缺语言回退策略（docs/06-D §9）：采用 S2「隐藏」——目标语言在本页没有独立可寻址
// 路径时不输出该语言链接（见 jet.go 的 BuildView 与装配层的 links 构造）。
package languages

import (
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.languages"

func init() { core.Register(&Component{}) }

// Component 语言切换器组件（原子，无 children）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema。
func (c *Component) PropsSpec() any { return &Props{} }

// Props 语言切换器属性。
//
// 说明：语言清单与链接地址不在这里配置——它们是站点级构建输入（project_locales +
// 本页逻辑路径），由装配层在构建期注入；组件只决定「怎么显示」。
type Props struct {
	// Orientation 排列方向：horizontal / vertical。
	Orientation string `json:"orientation,omitempty" ct:"select,horizontal=水平,vertical=垂直,sec=style,label=排列方向"`
	// Gap 项间距。
	Gap string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`
	// ShowCode 语言名后追加语言代码（如「English (en-US)」），便于运营核对。
	ShowCode bool `json:"showCode,omitempty" ct:"bool,sec=content,label=显示语言代码"`

	// --- 文字样式 ---
	Color        string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=文字色"`
	HoverColor   string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停文字色"`
	CurrentColor string `json:"currentColor,omitempty" ct:"color,maxlen=200,sec=style,label=当前语言色"`
	FontSize     string `json:"fontSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=字号"`
	FontWeight   string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,sec=style,label=字重"`
	ItemPadding  string `json:"itemPadding,omitempty" ct:"dimension,maxlen=20,sec=style,label=项内距"`

	// Advanced 通用高级属性（布局/边框/动效/响应式覆盖）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Validate 校验语言切换器节点（原子：不允许子节点）。
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

// compileCSS 生成语言切换器样式（三端；纯静态，无 hover 之外的交互）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	list := []string{"display: flex", "align-items: center", "flex-wrap: wrap", "list-style: none", "margin: 0", "padding: 0"}
	if p.Orientation == "vertical" {
		list = append(list, "flex-direction: column", "align-items: flex-start")
	}
	if p.Gap != "" {
		list = append(list, core.CSSDecl("gap", p.Gap))
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-lang-list", list)

	item := []string{"display: inline-flex", "align-items: center", "text-decoration: none", "transition: color .15s ease"}
	if p.Color != "" {
		item = append(item, core.CSSDecl("color", p.Color))
	}
	if p.FontSize != "" {
		item = append(item, core.CSSDecl("font-size", p.FontSize))
	}
	if p.FontWeight != "" {
		item = append(item, core.CSSDecl("font-weight", p.FontWeight))
	}
	if p.ItemPadding != "" {
		item = append(item, core.CSSDecl("padding", p.ItemPadding))
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-lang-link", item)
	// 当前语言不可点（span 而非 a），与链接同样的排版但明确区分。
	current := []string{"display: inline-flex", "align-items: center", "cursor: default"}
	current = append(current, item[2:]...)
	b.Add(core.BreakpointDesktop, sel+" .sky-lang-current", current)
	if p.HoverColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .sky-lang-link:hover", []string{core.CSSDecl("color", p.HoverColor)})
	}
	if p.CurrentColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .sky-lang-current", []string{core.CSSDecl("color", p.CurrentColor)})
	}
}
