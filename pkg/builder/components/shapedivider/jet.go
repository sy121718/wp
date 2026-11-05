package shapedivider

import (
	"strconv"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type LayerView struct {
	Class string

	D string

	Fill string

	FillOpacity string
}

type View struct {
	Layers []LayerView
}

func BuildView(p *Props) View {
	shape := effectiveShape(p)
	layers := effectiveLayers(p)
	drift := p.Animate == AnimDrift

	v := View{Layers: make([]LayerView, 0, layers)}
	for i := range layers {
		lv := LayerView{
			D: shapePath(shape, i, drift),
		}
		if i == 0 {

			lv.Fill = frontFill(p.Color)
		} else {
			if p.ColorBack != "" {
				lv.Fill = p.ColorBack
			} else {
				lv.Fill = frontFill(p.Color)
				lv.FillOpacity = backOpacity(i)
			}
			if drift {
				lv.Class = "sd-l" + strconv.Itoa(i+1)
			}
		}
		v.Layers = append(v.Layers, lv)
	}
	return v
}

func frontFill(color string) string {
	if color == "" {
		return "currentColor"
	}
	return color
}

func backOpacity(layer int) string {
	if layer >= 2 {
		return "0.3"
	}
	return "0.55"
}
