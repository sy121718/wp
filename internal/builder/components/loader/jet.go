package loader

import (
	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Self string

	Wrap bool

	Items []int

	Label string

	AriaText string
}

type shapeSpec struct {
	variant string

	self string

	wrap bool

	items int
}

var shapes = []shapeSpec{
	{variant: VariantSpinner, self: "ring"},
	{variant: VariantDots, self: "dot", items: 3},
	{variant: VariantBars, self: "bar", items: 4},
	{variant: VariantPulse, self: "pulse"},
	{variant: VariantPlane, self: "plane"},
	{variant: VariantGrid, self: "grid", wrap: true, items: 9},
	{variant: VariantOrbit, self: "orbit"},
	{variant: VariantWave, self: "wave", wrap: true, items: 5},
	{variant: VariantBounce, self: "bounce", items: 3},
}

var shapeByVariant = buildShapeIndex(shapes)

func buildShapeIndex(list []shapeSpec) map[string]shapeSpec {
	m := make(map[string]shapeSpec, len(list))
	for _, s := range list {
		if _, dup := m[s.variant]; dup {
			panic("loader: 重复的形态: " + s.variant)
		}
		m[s.variant] = s
	}
	return m
}

func BuildView(p *Props) View {
	v := View{AriaText: "加载中", Label: p.Label}
	spec := shapeByVariant[effectiveVariant(p)]
	v.Self, v.Wrap = spec.self, spec.wrap
	if spec.items > 0 {
		v.Items = make([]int, spec.items)
	}
	return v
}
