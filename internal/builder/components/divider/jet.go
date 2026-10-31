package divider

import (
	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	IsInset bool

	IsText bool

	InsetText string

	InsetIconContent string
}

func BuildView(p *Props) View {
	v := View{IsInset: p.Inset.Kind != InsetNone && p.Inset.Kind != ""}
	if v.IsInset {
		if p.Inset.Kind == InsetText {
			v.IsText = true
			v.InsetText = p.Inset.Text
		} else if inner, ok := core.IconInner(insetIconNames[p.Inset.IconName]); ok {
			v.InsetIconContent = inner
		}
	}
	return v
}
