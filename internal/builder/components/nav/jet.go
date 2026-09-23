// nav — Jet 渲染路径辅助导出。
package nav

import (
	"html/template"
	"strings"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出导航样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// ItemView 单个菜单项的渲染视图。
type ItemView struct {
	Label       string
	URL         string
	Target      string
	HasChildren bool
	// Current 当前页高亮（构建期按 RenderContext.CurrentPath 标记）。
	Current  bool
	Children []ItemView
	// HasPanel 该项有悬浮面板（超级菜单：面板内容来自全局块，构建期展开）。
	HasPanel bool
	// PanelHTML 面板内容（已渲染的 HTML；模板直接输出，不再二次转义）。
	PanelHTML template.HTML
	// PanelFull 面板通栏（宽度 full）。
	PanelFull bool
}

// View 导航渲染视图。
type View struct {
	Items          []ItemView
	ToggleID       string
	MobileCollapse bool
	ToggleLabel    string
	// Label 导航容器 aria-label（构建期按当前语言填充，多语言 P4）。
	Label string
	// SubmenuToggleLabel 触屏子菜单展开控件的无障碍名后缀。
	//
	// 触屏没有悬停，子菜单改由 label + checkbox 的「点击展开」驱动（见 nav.css 的
	// @hovernone 块）；那个 checkbox 是视觉隐藏的 sr-only 控件，读屏与键盘用户只能
	// 靠可访问名识别它，所以文案不能为空。模板里与菜单项文字拼成「父项名 子菜单」。
	SubmenuToggleLabel string
	// ToggleIcon 移动端汉堡按钮图标（基座图标库 menu，完整 <svg>；aria-hidden）。
	ToggleIcon string
	// ChevronIcon 子菜单 / 面板的展开箭头（基座图标库 chevron-down，完整 <svg>）。
	//
	// 以前模板里直接写字符 '▾' —— 字符图标在不同字体下字宽与基线不一致，
	// 也无法与图标库的描边风格保持一致（同一页面上两种箭头会明显不同）。
	ChevronIcon string
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyLabel 导航容器 aria-label 的词条 key。
	TextKeyLabel = "site.component.nav.label"
)

// textFallbackLabel 缺词条时的原中文兜底（绝不输出空串）。
const textFallbackLabel = "站点导航"

// ApplyI18n 按当前语言填充导航无障碍标签（实现 core.I18nAware）。
// text 为 nil 或未命中词条时使用包内中文兜底，保证属性永不为空。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Label = textFallbackLabel
		return
	}
	v.Label = text(TextKeyLabel, textFallbackLabel)
}

// BuildView 生成导航视图（递归展开菜单项）。
func BuildView(node *core.Node, p *Props) View {
	// ToggleLabel 只作**无障碍名**（模板里进 sr-only）：可见的汉堡图标由图标库
	// 的 SVG 承担 —— 以前它是字符 '☰'，既当可见文本又当无障碍名，
	// 结果按钮在不同字体下粗细不一，也无法与其它图标统一描边。
	label := p.ToggleLabel
	if label == "" {
		label = "菜单"
	}
	view := View{
		Items:          itemViews(p.Items),
		ToggleID:       "sky-nav-toggle-" + node.ID,
		MobileCollapse: p.MobileCollapse,
		ToggleLabel:    label,
		// 不做逐语言取词：它是无障碍名后缀，随菜单项文字一起播报，
		// 缺译文时退回中文原文即可（与 ToggleLabel 的兜底同一套思路）。
		SubmenuToggleLabel: "子菜单",
	}
	if svg, ok := core.IconSVGClass("menu", "sky-nav-burger-icon"); ok {
		view.ToggleIcon = svg
	}
	if svg, ok := core.IconSVGClass("chevron-down", "sky-nav-chevron-icon"); ok {
		view.ChevronIcon = svg
	}
	return view
}

// ValidateItems 导出菜单项校验：构建期按导航位置解析出菜单后二次校验
// （层级/数量/链接协议与手写菜单同一套规则）。
func ValidateItems(items []Item, nodeID string) error { return validateItems(items, nodeID, 0) }

// MarkCurrent 按当前页面路径标记菜单项（构建期，产物仍是静态 HTML）。
// 匹配规则：去掉尾部斜杠后精确相等；"/" 与 "" 视为同一首页。
func MarkCurrent(items []Item, currentPath string) {
	cur := normalizePath(currentPath)
	if cur == "" {
		return
	}
	for i := range items {
		if normalizePath(items[i].URL) == cur {
			items[i].Current = true
		}
		MarkCurrent(items[i].Children, currentPath)
	}
}

// normalizePath 去掉尾部斜杠；空路径与 "/" 归一为 "/"。
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return "/"
	}
	return p
}

// ItemsOf 把构建期导航解析结果（core.NavigationItem）转换为菜单项。
// 供 builder 的 navViewOf 在绑定导航位置时覆盖手写 Items。
func ItemsOf(items []core.NavigationItem) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		out = append(out, Item{
			Label:        it.Label,
			URL:          it.URL,
			Target:       it.Target,
			Children:     ItemsOf(it.Children),
			PanelBlockID: it.PanelBlockID,
			PanelWidth:   it.PanelWidth,
		})
	}
	return out
}

// itemViews 递归转换菜单项。
func itemViews(items []Item) []ItemView {
	out := make([]ItemView, 0, len(items))
	for _, it := range items {
		out = append(out, ItemView{
			Label:       it.Label,
			URL:         it.URL,
			Target:      it.Target,
			HasChildren: len(it.Children) > 0,
			Current:     it.Current,
			Children:    itemViews(it.Children),
			HasPanel:    it.PanelHTML != "",
			PanelHTML:   it.PanelHTML,
			PanelFull:   it.PanelWidth == "full",
		})
	}
	return out
}
