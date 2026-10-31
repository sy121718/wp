package nav

import (
	"html/template"
	"strings"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type ItemView struct {
	Label       string
	URL         string
	Target      string
	HasChildren bool

	Current  bool
	Children []ItemView

	HasPanel bool

	PanelHTML template.HTML

	PanelFull bool
}

type View struct {
	Items          []ItemView
	ToggleID       string
	MobileCollapse bool
	ToggleLabel    string

	Label string

	SubmenuToggleLabel string

	ToggleIcon string

	ChevronIcon string
}

const (
	TextKeyLabel = "site.component.nav.label"
)

const textFallbackLabel = "站点导航"

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

func BuildView(node *core.Node, p *Props) View {

	label := p.ToggleLabel
	if label == "" {
		label = "菜单"
	}
	view := View{
		Items:          itemViews(p.Items),
		ToggleID:       "sky-nav-toggle-" + node.ID,
		MobileCollapse: p.MobileCollapse,
		ToggleLabel:    label,

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

func ValidateItems(items []Item, nodeID string) error { return validateItems(items, nodeID, 0) }

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
