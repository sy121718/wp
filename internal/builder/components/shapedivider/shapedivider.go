package shapedivider

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.shapedivider"

const (
	ShapeWave     = "wave"
	ShapeCurve    = "curve"
	ShapeSlope    = "slope"
	ShapeTilt     = "tilt"
	ShapeTriangle = "triangle"
	ShapeRound    = "round"
)

const (
	AnimNone  = "none"
	AnimDrift = "drift"
)

type Height struct {
	Desktop string `json:"desktop,omitempty" ct:"dimension,maxlen=20,sec=style,label=桌面端高度"`
	Tablet  string `json:"tablet,omitempty" ct:"dimension,maxlen=20,sec=style,label=平板端高度"`
	Mobile  string `json:"mobile,omitempty" ct:"dimension,maxlen=20,sec=style,label=手机端高度"`
}

type Props struct {
	Shape string `json:"shape,omitempty" ct:"select,wave=波浪,curve=弧线,slope=斜坡,tilt=倾斜,triangle=三角,round=圆弧,default=wave,sec=content,label=形状"`

	Layers int `json:"layers,omitempty" ct:"int,min=0,max=3,sec=content,label=层数（波浪/弧线）"`

	FlipX bool `json:"flipX,omitempty" ct:"bool,sec=transform,label=水平翻转"`
	FlipY bool `json:"flipY,omitempty" ct:"bool,sec=transform,label=垂直翻转"`

	Height Height `json:"height,omitempty"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=前景颜色"`

	ColorBack string `json:"colorBack,omitempty" ct:"color,maxlen=200,sec=style,label=背景层颜色"`

	Animate string `json:"animate,omitempty" ct:"select,none=无,drift=层漂移,default=none,sec=style,label=动画"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "形状分隔线",
		Hint:            "区块过渡装饰（波浪/弧线/斜坡）",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"shape": "wave",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

func multiLayerShape(shape string) bool {
	return shape == ShapeWave || shape == ShapeCurve
}

func effectiveShape(p *Props) string {
	if p.Shape == "" {
		return ShapeWave
	}
	return p.Shape
}

func effectiveLayers(p *Props) int {
	if p.Layers < 1 {
		return 1
	}
	if p.Layers > 3 {
		return 3
	}
	return p.Layers
}

func validateExtra(p *Props, nodeID string) (err error) {
	shape := effectiveShape(p)
	switch shape {
	case ShapeWave, ShapeCurve, ShapeSlope, ShapeTilt, ShapeTriangle, ShapeRound:
	default:
		return fmt.Errorf("无效的形状: %q", p.Shape)
	}
	if p.Layers < 0 || p.Layers > 3 {
		return fmt.Errorf("层数越界: %d（1~3）", p.Layers)
	}
	layers := effectiveLayers(p)
	if layers > 1 && !multiLayerShape(shape) {
		return fmt.Errorf("形状 %q 仅支持单层（多层仅 wave/curve）", shape)
	}
	switch p.Animate {
	case "", AnimNone:
	case AnimDrift:
		if shape != ShapeWave {
			return fmt.Errorf("层漂移动画仅 wave 形状支持（当前 %q）", shape)
		}
		if layers < 2 {
			return fmt.Errorf("层漂移动画需要 2 层以上（当前 %d 层）", layers)
		}
	default:
		return fmt.Errorf("无效的动画取值: %q", p.Animate)
	}
	for _, f := range []struct {
		name string
		v    string
	}{{"桌面端高度", p.Height.Desktop}, {"平板端高度", p.Height.Tablet}, {"手机端高度", p.Height.Mobile}} {
		if f.v != "" && !core.IsSafeCSSValue(f.v) {
			return fmt.Errorf("无效的%s: %q", f.name, f.v)
		}
	}
	if err := checkFill("前景颜色", p.Color); err != nil {
		return err
	}
	if err := checkFill("背景层颜色", p.ColorBack); err != nil {
		return err
	}
	return nil
}

func checkFill(name, v string) error {
	if v == "" {
		return nil
	}
	if !core.IsSafeCSSValue(v) || strings.Contains(v, "url(") {
		return fmt.Errorf("无效的%s: %q", name, v)
	}
	return nil
}

//go:embed shapedivider.css
var shapedividerCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	hd, ht, hm := p.Height.Desktop, p.Height.Tablet, p.Height.Mobile
	if hd == "" {
		hd = "120px"
	}
	if ht == "" {
		ht = "90px"
	}
	if hm == "" {
		hm = "64px"
	}

	vars := map[string]string{
		"flip":      strings.Join(flipDecls(p), "; "),
		"h_desktop": hd,
		"h_tablet":  ht,
		"h_mobile":  hm,
		"drift":     core.BoolVar(p.Animate == AnimDrift),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, shapedividerCSS, vars); err != nil {

		panic(fmt.Sprintf("shapedivider 组件样式解析失败: %v", err))
	}
}

func flipDecls(p *Props) []string {
	switch {
	case p.FlipX && p.FlipY:
		return []string{"transform: scaleX(-1) scaleY(-1)"}
	case p.FlipX:
		return []string{"transform: scaleX(-1)"}
	case p.FlipY:
		return []string{"transform: scaleY(-1)"}
	default:
		return nil
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("shapedivider", shapedividerTemplate)
}

//go:embed shapedivider.jet
var shapedividerTemplate string
