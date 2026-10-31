package spacer

import (
	_ "embed"
	"fmt"

	"go_wp/internal/builder/core"
)

//go:embed spacer.css
var spacerCSS string

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{
		"h_desktop": p.Height.Desktop,
		"h_tablet":  p.Height.Tablet,
		"h_mobile":  p.Height.Mobile,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, spacerCSS, vars); err != nil {

		panic(fmt.Sprintf("spacer 组件样式解析失败: %v", err))
	}
}

type View struct{}

func BuildView(p *Props) View {
	return View{}
}
