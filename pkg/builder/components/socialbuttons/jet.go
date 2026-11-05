package socialbuttons

import (
	"strings"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type ItemView struct {
	Platform string

	URL string

	SVGContent string

	HasIcon bool

	Initial string
}

type View struct {
	Color string

	Items []ItemView
}

func BuildView(p *Props) View {
	color := p.Color
	if color == "" {
		color = ColorBrand
	}
	items := make([]ItemView, 0, len(p.Items))
	for _, it := range p.Items {
		iv := ItemView{Platform: it.Platform, URL: it.URL}
		if svg, ok := platformIcons[it.Platform]; ok {
			iv.HasIcon = true
			iv.SVGContent = svg
		} else {

			iv.Initial = strings.ToUpper(it.Platform[:1])
		}
		items = append(items, iv)
	}
	return View{Color: string(color), Items: items}
}
