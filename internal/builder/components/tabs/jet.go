package tabs

import (
	"strconv"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type TabView struct {
	ID string

	Name string

	Checked bool

	Label string
}

type View struct {
	Tabs []TabView

	Vertical bool
}

func BuildView(node *core.Node, p *Props) View {
	tabs := make([]TabView, 0, len(p.Tabs))
	for i, t := range p.Tabs {
		tabs = append(tabs, TabView{
			ID:      "sky-tabs-" + node.ID + "-" + strconv.Itoa(i),
			Name:    "sky-tabs-" + node.ID,
			Checked: i == 0,
			Label:   t.Label,
		})
	}
	return View{Tabs: tabs, Vertical: p.Vertical}
}
