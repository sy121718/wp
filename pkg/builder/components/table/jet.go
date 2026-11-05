package table

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Caption string

	Headers []string

	Rows [][]string

	HasCaption bool

	HasHeader bool

	HasBody bool
}

func BuildView(p *Props) View {
	return View{
		Caption:    p.Caption,
		Headers:    p.Headers,
		Rows:       p.Rows,
		HasCaption: p.Caption != "",
		HasHeader:  len(p.Headers) > 0,
		HasBody:    len(p.Rows) > 0,
	}
}
