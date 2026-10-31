package list

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type ItemView struct {
	MarkerHTML string

	IsLink bool

	LinkHref string

	Text string
}

type View struct {
	Style string

	Items []ItemView
}

func BuildView(p *Props, siteLink func(string) string) View {
	style := p.Style
	if style == "" {
		style = StyleIcon
	}
	items := make([]ItemView, 0, len(p.Items))
	for i, item := range p.Items {
		iv := ItemView{Text: item.Text}
		switch style {
		case StyleIcon:
			svg, ok := core.IconSVG(item.Icon)
			if !ok {
				svg, _ = core.IconSVG("check")
			}
			iv.MarkerHTML = svg
		case StyleNumber:
			iv.MarkerHTML = fmt.Sprintf("%d", i+1)
		default:
			iv.MarkerHTML = `<i class="sky-list-dot"></i>`
		}
		if strings.TrimSpace(item.Link) != "" {
			iv.IsLink = true

			iv.LinkHref = core.SiteLinkOrSame(siteLink, item.Link)
		}
		items = append(items, iv)
	}
	return View{Style: string(style), Items: items}
}
