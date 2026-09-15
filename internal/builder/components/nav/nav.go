// Package nav 实现 core.nav：站点导航菜单组件（对标 WordPress 前台菜单）。
//
// 能力对齐 WP「外观 → 菜单」：多级菜单项（一级 + 二级）、每项独立链接与打开方式、
// 水平/垂直排列、悬停与当前项样式、子菜单浮层；移动端可折叠为汉堡菜单（纯 CSS，零 JS）。
// 静态构建：菜单项写在 props.items 里，构建期直接输出 <nav><ul>…
package nav

import (
	_ "embed" // nav.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.nav"

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("nav", navTemplate)
}

// Component 导航菜单组件（内容型，无 children 节点）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b）。
//
// **只对「自定义菜单」模式有效**：那种模式下 label 写在页面文档的 Props 里，
// 作者在页面翻译工作台里逐条填译文。
//
// Menu 模式（header/footer）的 label 来自 navigation 模块的节点，**不在 Props 里** ——
// 页面翻译工作台既看不到它，这个白名单对它也无效（审计 I18N-018 的冲突就在这：
// 两种模式的 label 长得一样，维护位置却完全不同）。它的译文由构建期的导航解析器补：
// pipeline.NavigationAdapter 用 sys_translation 的 navigation.label 语境回填，
// 维护入口在导航管理页。
//
// 位置不同是刻意的：菜单的归属决定它归谁维护 —— 全局页眉导航改一处就该全站生效，
// 绑进某一页的翻译里反而会出现「改了这页、那页没变」。
//
// 原注释：
// docs/06-D §7.5 决策 F6）。只有这里列出的字段参与内容翻译，未声明字段永不翻译。
func (c *Component) Translatable() []string { return []string{"label"} }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema。
func (c *Component) PropsSpec() any { return &Props{} }

// Item 菜单项（支持一级子菜单，对齐 WP 菜单的父子层级）。
type Item struct {
	Label    string `json:"label,omitempty"`
	URL      string `json:"url,omitempty"`
	Target   string `json:"target,omitempty"` // self / blank
	Children []Item `json:"children,omitempty"`
	// Current 是否当前页（构建期按页面路径标记，输出 is-current 类，不入文档）。
	Current bool `json:"-"`
}

// Props 导航菜单属性。
type Props struct {
	// Menu 绑定导航位置：非空（header/footer）时构建期用该位置的导航菜单
	// 覆盖 Items（navigation 模块数据，构建期解析，产物仍为静态）。
	// 为空表示用下方 Items 手写菜单项。
	Menu string `json:"menu,omitempty" ct:"select,=自定义菜单,header=页眉导航,footer=页脚导航,sec=content,label=导航位置"`

	// Items 菜单项列表（一级；每项可带 Children 形成二级菜单）。
	Items []Item `json:"items,omitempty"`

	// --- 布局 ---
	Orientation string `json:"orientation,omitempty" ct:"select,horizontal=水平,vertical=垂直,sec=style,label=排列方向"`
	Gap         string `json:"gap,omitempty" ct:"dimension,maxlen=20,sec=style,label=项间距"`
	Align       string `json:"align,omitempty" ct:"select,left=靠左,center=居中,right=靠右,sec=style,label=整体对齐"`

	// --- 文字 ---
	Color       string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=文字色"`
	HoverColor  string `json:"hoverColor,omitempty" ct:"color,maxlen=200,sec=style,label=悬停文字色"`
	ActiveColor string `json:"activeColor,omitempty" ct:"color,maxlen=200,sec=style,label=当前项颜色"`
	FontSize    string `json:"fontSize,omitempty" ct:"dimension,maxlen=20,sec=style,label=字号"`
	FontWeight  string `json:"fontWeight,omitempty" ct:"select,400=常规,500=中等,600=半粗,700=粗体,sec=style,label=字重"`
	ItemPadding string `json:"itemPadding,omitempty" ct:"dimension,maxlen=20,sec=style,label=项内距"`

	// --- 子菜单 ---
	SubmenuBg    string `json:"submenuBg,omitempty" ct:"color,maxlen=200,sec=style,label=子菜单底色"`
	SubmenuWidth string `json:"submenuWidth,omitempty" ct:"dimension,maxlen=20,sec=style,label=子菜单最小宽度"`

	// --- 移动端 ---
	MobileCollapse bool   `json:"mobileCollapse,omitempty" ct:"bool,sec=style,label=移动端折叠为汉堡菜单"`
	ToggleLabel    string `json:"toggleLabel,omitempty" ct:"safe,maxlen=20,sec=style,label=折叠按钮文字"`

	// Advanced 通用高级属性（布局/边框/动效/响应式覆盖）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Validate 校验菜单节点。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
	var p Props
	if err = json.Unmarshal(node.Props, &p); err != nil {
		return fmt.Errorf("节点 %s props 解析失败: %w", node.ID, err)
	}
	if err = validateItems(p.Items, node.ID, 0); err != nil {
		return err
	}
	if err = core.ValidateAdvanced(core.AdvancedOf(&p), node.ID, ids); err != nil {
		return err
	}
	return core.ValidateSpec(&p, node.ID)
}

// validateItems 递归校验菜单项（层级上限 2，链接白名单）。
func validateItems(items []Item, nodeID string, depth int) (err error) {
	if len(items) == 0 {
		return nil
	}
	if depth > 1 {
		return fmt.Errorf("节点 %s: 菜单最多支持两级", nodeID)
	}
	if len(items) > 50 {
		return fmt.Errorf("节点 %s: 菜单项过多（上限 50）", nodeID)
	}
	for i, it := range items {
		label := strings.TrimSpace(it.Label)
		if label == "" {
			return fmt.Errorf("节点 %s: 第 %d 个菜单项缺少文字", nodeID, i+1)
		}
		if len([]rune(label)) > 60 {
			return fmt.Errorf("节点 %s: 第 %d 个菜单项文字过长", nodeID, i+1)
		}
		if u := strings.TrimSpace(it.URL); u != "" {
			if len(u) > 300 || !core.IsSafeURL(u) {
				return fmt.Errorf("节点 %s: 第 %d 个菜单项链接非法: %q", nodeID, i+1, u)
			}
		}
		if it.Target != "" && it.Target != "self" && it.Target != "blank" {
			return fmt.Errorf("节点 %s: 第 %d 个菜单项打开方式非法: %q", nodeID, i+1, it.Target)
		}
		if err = validateItems(it.Children, nodeID, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// navCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed nav.css
var navCSS string

// compileCSS 生成导航样式（三端）。
//
// Go 侧只做兜底与映射：子菜单底色未配时跟主题面、对齐档位翻成 flex 值、
// 竖向与移动端折叠翻成布尔量。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 子菜单底色未配置时跟主题面（写死的话换主题它不动）。
	submenuBg := p.SubmenuBg
	if submenuBg == "" {
		submenuBg = "var(--sky-c-surface, #fff)"
	}
	justify := map[string]string{"left": "flex-start", "center": "center", "right": "flex-end"}[p.Align]

	vars := map[string]string{
		"vertical":       core.BoolVar(p.Orientation == "vertical"),
		"gap":            p.Gap,
		"justify":        justify,
		"color":          p.Color,
		"fontSize":       p.FontSize,
		"fontWeight":     p.FontWeight,
		"itemPadding":    p.ItemPadding,
		"hoverColor":     p.HoverColor,
		"activeColor":    p.ActiveColor,
		"submenuBg":      submenuBg,
		"submenuWidth":   p.SubmenuWidth,
		"mobileCollapse": core.BoolVar(p.MobileCollapse),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, navCSS, vars); err != nil {
		panic(fmt.Sprintf("nav 组件样式解析失败: %v", err))
	}
}

// navTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed nav.jet
var navTemplate string
