package loader

import (
	_ "embed"
	"fmt"
	"strconv"

	"go_wp/internal/builder/core"
)

const Type = "core.loader"

const (
	VariantSpinner = "spinner"
	VariantDots    = "dots"
	VariantBars    = "bars"
	VariantPulse   = "pulse"
	VariantPlane   = "plane"
	VariantGrid    = "grid"
	VariantOrbit   = "orbit"
	VariantWave    = "wave"
	VariantBounce  = "bounce"
)

type Props struct {
	Variant string `json:"variant,omitempty" ct:"select,spinner=环形,dots=点阵,bars=条形,pulse=脉冲,plane=平面翻转,grid=九宫格,orbit=环绕,wave=波浪,bounce=弹跳,default=spinner,sec=content,label=形态"`

	Size string `json:"size,omitempty" ct:"dimension,maxlen=20,sec=style,label=尺寸"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`

	Label string `json:"label,omitempty" ct:"text,maxlen=100,sec=content,label=文本"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "加载指示",
		Hint:            "纯 CSS 加载动画（九种形态）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"variant": "spinner",
			"size":    "32px",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
		Translatable:  []string{"label"},
	},
}

func effectiveVariant(p *Props) string {
	if p.Variant == "" {
		return VariantSpinner
	}
	return p.Variant
}

func validateExtra(p *Props, nodeID string) (err error) {
	if _, ok := shapeByVariant[effectiveVariant(p)]; !ok {
		return fmt.Errorf("无效的加载器形态: %q", p.Variant)
	}
	if p.Size != "" && !core.IsSafeCSSValue(p.Size) {
		return fmt.Errorf("无效的尺寸: %q", p.Size)
	}
	if p.Color != "" && !core.IsSafeCSSValue(p.Color) {
		return fmt.Errorf("无效的颜色: %q", p.Color)
	}
	return nil
}

//go:embed loader.css
var loaderCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	size := p.Size
	if size == "" {
		size = "32px"
	}
	variant := effectiveVariant(p)

	delays := func(vals ...string) []map[string]string {
		out := make([]map[string]string, 0, len(vals))
		for i, d := range vals {
			out = append(out, map[string]string{"nth": strconv.Itoa(i + 1), "delay": d})
		}
		return out
	}

	shifted := func(vals ...string) []map[string]string {
		out := make([]map[string]string, 0, len(vals))
		for i, d := range vals {
			out = append(out, map[string]string{"nth": strconv.Itoa(i + 2), "delay": d})
		}
		return out
	}

	gridDelays := make([]map[string]string, 0, 9)
	for i, d := range []float64{0, .1, .2, .1, .2, .3, .2, .3, .4} {
		gridDelays = append(gridDelays, map[string]string{
			"nth":   strconv.Itoa(i + 1),
			"delay": fmt.Sprintf("%.1fs", d),
		})
	}

	vars := map[string]string{
		"size":     size,
		"color":    p.Color,
		"spinner":  core.BoolVar(variant == VariantSpinner),
		"dots":     core.BoolVar(variant == VariantDots),
		"bars":     core.BoolVar(variant == VariantBars),
		"pulse":    core.BoolVar(variant == VariantPulse),
		"plane":    core.BoolVar(variant == VariantPlane),
		"grid":     core.BoolVar(variant == VariantGrid),
		"orbit":    core.BoolVar(variant == VariantOrbit),
		"wave":     core.BoolVar(variant == VariantWave),
		"bounce":   core.BoolVar(variant == VariantBounce),
		"hasLabel": core.BoolVar(p.Label != ""),
	}
	lists := map[string][]map[string]string{
		"dotsDelays":   shifted(".15s", ".3s"),
		"barsDelays":   shifted(".15s", ".3s", ".45s"),
		"gridDelays":   gridDelays,
		"waveDelays":   delays("-1.2s", "-1.1s", "-1s", "-.9s", "-.8s"),
		"bounceDelays": delays("-.32s", "-.16s", "0s"),
	}
	if err := core.ApplyComponentCSSTmplLists(b, sel, loaderCSS, vars, lists); err != nil {
		panic(fmt.Sprintf("loader 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("loader", loaderTemplate)
}

//go:embed loader.jet
var loaderTemplate string
