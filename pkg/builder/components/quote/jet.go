package quote

import (
	"go_wp/pkg/richtext"
	"strings"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Text string

	Author string

	Source string

	HasAuthor bool

	HasSource bool
}

func BuildView(p *Props, siteLink func(string) string) View {
	source := p.Source
	if strings.TrimSpace(source) != "" {
		source = core.SiteLinkOrSame(siteLink, source)
	}
	return View{
		Text:      richtext.RichTextHTML(p.Text),
		Author:    p.Author,
		Source:    source,
		HasAuthor: p.Author != "",
		HasSource: p.Source != "",
	}
}
