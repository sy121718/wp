package progress

import (
	"strconv"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	ValueNow string

	ValueMax string

	Label string

	HasLabel bool
}

func BuildView(p *Props) View {
	return View{
		ValueNow: strconv.Itoa(p.Value),
		ValueMax: strconv.Itoa(effectiveMax(p)),
		Label:    p.Label,
		HasLabel: p.Label != "",
	}
}
