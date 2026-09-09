// nav — Jet 渲染路径辅助导出。
package nav

import (
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
}

// View 导航渲染视图。
type View struct {
	Items          []ItemView
	ToggleID       string
	MobileCollapse bool
	ToggleLabel    string
	// Label 导航容器 aria-label（构建期按当前语言填充，多语言 P4）。
	Label string
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
	label := p.ToggleLabel
	if label == "" {
		label = "☰"
	}
	return View{
		Items:          itemViews(p.Items),
		ToggleID:       "wp-nav-toggle-" + node.ID,
		MobileCollapse: p.MobileCollapse,
		ToggleLabel:    label,
	}
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
			Label:    it.Label,
			URL:      it.URL,
			Target:   it.Target,
			Children: ItemsOf(it.Children),
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
		})
	}
	return out
}
