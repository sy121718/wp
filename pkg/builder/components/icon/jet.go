package icon

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	IconSVG string
}

func BuildView(p *Props) View {
	name := p.IconName
	if name == "" {
		name = defaultIconName
	}
	path, ok := iconPath(name)
	if !ok {
		path, _ = iconPath(defaultIconName)
	}
	return View{IconSVG: path}
}
