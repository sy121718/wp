// Package nav 实现 core.nav：站点导航菜单组件（对标 WordPress 前台菜单）。
//
// 能力对齐 WP「外观 → 菜单」：多级菜单项（一级 + 二级）、每项独立链接与打开方式、
// 水平/垂直排列、悬停与当前项样式、子菜单浮层；移动端可折叠为汉堡菜单（纯 CSS，零 JS）。
// 静态构建：菜单项写在 props.items 里，构建期直接输出 <nav><ul>…
package nav

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.nav"

func init() { core.Register(&Component{}) }

// Component 导航菜单组件（内容型，无 children 节点）。
type Component struct{}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// Translatable 实现 core.TranslatableProvider：可翻译字段白名单（多语言 P5b，
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

// compileCSS 生成导航样式（三端）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	list := []string{"display: flex", "align-items: center", "list-style: none", "margin: 0", "padding: 0"}
	if p.Orientation == "vertical" {
		list = append(list, "flex-direction: column", "align-items: flex-start")
	}
	if p.Gap != "" {
		list = append(list, core.CSSDecl("gap", p.Gap))
	}
	if v := map[string]string{"left": "flex-start", "center": "center", "right": "flex-end"}[p.Align]; v != "" {
		list = append(list, "justify-content: "+v)
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-nav-list", list)
	b.Add(core.BreakpointDesktop, sel+" .sky-nav-item", []string{"position: relative", "margin: 0"})

	link := []string{"display: inline-flex", "align-items: center", "text-decoration: none", "transition: color .15s ease"}
	if p.Color != "" {
		link = append(link, core.CSSDecl("color", p.Color))
	}
	if p.FontSize != "" {
		link = append(link, core.CSSDecl("font-size", p.FontSize))
	}
	if p.FontWeight != "" {
		link = append(link, core.CSSDecl("font-weight", p.FontWeight))
	}
	if p.ItemPadding != "" {
		link = append(link, core.CSSDecl("padding", p.ItemPadding))
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-nav-item > a", link)
	if p.HoverColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-item > a:hover", []string{core.CSSDecl("color", p.HoverColor)})
	}
	if p.ActiveColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-item.is-current > a", []string{core.CSSDecl("color", p.ActiveColor)})
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-sub-item.is-current > a", []string{core.CSSDecl("color", p.ActiveColor)})
	}

	sub := []string{
		"position: absolute", "top: 100%", "left: 0", "z-index: 20",
		"min-width: 180px", "list-style: none", "margin: 0", "padding: 8px 0",
		"border-radius: 8px", "box-shadow: 0 8px 24px rgba(0,0,0,.12)",
		"opacity: 0", "visibility: hidden", "transition: opacity .15s ease, visibility .15s ease",
	}
	if p.SubmenuBg != "" {
		sub = append(sub, core.CSSDecl("background", p.SubmenuBg))
	} else {
		sub = append(sub, "background: #fff")
	}
	if p.SubmenuWidth != "" {
		sub = append(sub, "min-width: "+p.SubmenuWidth)
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-nav-sub", sub)
	b.Add(core.BreakpointDesktop, sel+" .sky-nav-item:hover > .sky-nav-sub", []string{"opacity: 1", "visibility: visible"})
	b.Add(core.BreakpointDesktop, sel+" .sky-nav-sub a", []string{
		"display: block", "padding: 8px 16px", "text-decoration: none", "color: inherit", "white-space: nowrap",
	})
	if p.HoverColor != "" {
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-sub a:hover", []string{core.CSSDecl("color", p.HoverColor)})
	}

	if p.MobileCollapse {
		// 折叠开关：checkbox 必须**可聚焦**，所以用 sr-only 而不是模板里的 hidden 属性 ——
		// hidden 的元素不进键盘序列，键盘用户无法展开移动端菜单（触屏之外全废）。
		// sr-only 的 checkbox 仍是原生控件：空格键切换、读屏能播报「已选中/未选中」。
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-toggle", []string{
			"position: absolute", "width: 1px", "height: 1px", "margin: -1px",
			"padding: 0", "border: 0", "clip-path: inset(50%)", "overflow: hidden", "white-space: nowrap",
		})
		// 聚焦可见：焦点环画在汉堡按钮上（键盘用户看得到当前位置）。
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-toggle:focus-visible + .sky-nav-burger", []string{
			"outline: 2px solid var(--sky-c-primary, #2563eb)", "outline-offset: 2px",
		})
		b.Add(core.BreakpointDesktop, sel+" .sky-nav-burger", []string{
			"display: none", "cursor: pointer", "font-size: 22px", "line-height: 1", "padding: 8px 12px",
		})
		b.Add(core.BreakpointMobile, sel+" .sky-nav-burger", []string{"display: block"})
		b.Add(core.BreakpointMobile, sel+" .sky-nav-list", []string{
			"display: none", "flex-direction: column", "align-items: stretch", "gap: 0", "width: 100%", "padding: 8px 0",
		})
		b.Add(core.BreakpointMobile, sel+" .sky-nav-toggle:checked ~ .sky-nav-list", []string{"display: flex"})
		b.Add(core.BreakpointMobile, sel+" .sky-nav-item > a", []string{"padding: 10px 12px", "width: 100%"})
		b.Add(core.BreakpointMobile, sel+" .sky-nav-sub", []string{
			"position: static", "opacity: 1", "visibility: visible", "box-shadow: none",
			"padding: 0 0 0 16px", "background: transparent",
		})
	}
}
