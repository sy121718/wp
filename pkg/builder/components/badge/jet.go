package badge

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Text string
}

func BuildView(p *Props) View {
	return View{Text: p.Text}
}
