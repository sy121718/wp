package marquee

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	PauseOnHover bool

	Copies []int
}

func BuildView(p *Props) View {
	return View{PauseOnHover: p.PauseOnHover, Copies: []int{0, 1}}
}
