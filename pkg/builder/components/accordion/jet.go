package accordion

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type ItemView struct {
	Title string

	Open bool
}

type View struct {
	Items []ItemView

	OneOpen bool

	Borderless bool

	ChevronIcon string
}

func BuildView(p *Props) View {
	items := make([]ItemView, 0, len(p.Items))

	opened := false
	for _, it := range p.Items {
		open := it.Open
		if p.OneOpen && open {
			if opened {
				open = false
			}
			opened = true
		}
		items = append(items, ItemView{Title: it.Title, Open: open})
	}
	view := View{Items: items, OneOpen: p.OneOpen, Borderless: p.Borderless}
	if svg, ok := core.IconSVGClass("chevron-down", "sky-accordion-chevron"); ok {
		view.ChevronIcon = svg
	}
	return view
}
