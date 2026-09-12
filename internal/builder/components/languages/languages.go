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
	_ "embed" // languages.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.languages"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("languages", languagesTemplate)
}

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

// languagesCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed languages.css
var languagesCSS string

// compileCSS 生成语言切换器样式（纯静态，无 hover 之外的交互）。
//
// 「当前语言」与「语言链接」的排版逐条一致（迁移前 Go 侧就是复用同一个切片），
// 差别只有 cursor 与不可点 —— 这条一致性在样式源里是两段重复的声明，刻意保留重复，
// 因为它比「共用一段」更能让人一眼看出两者的关系与差异。
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
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("languages 组件样式解析失败: %v", err))
	}
}

// languagesTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed languages.jet
var languagesTemplate string
