package badge

import (
	_ "embed"
	"fmt"

	"go_wp/internal/builder/core"
)

const Type = "core.badge"

const (
	VariantSolid   = "solid"
	VariantOutline = "outline"
	VariantSoft    = "soft"
)

const defaultColor = "var(--sky-c-primary, #2563eb)"

type Props struct {
	Text string `json:"text,omitempty" ct:"text,maxlen=50,sec=content,label=文字"`

	Variant string `json:"variant,omitempty" ct:"select,solid=实心,outline=描边,soft=浅底,default=solid,sec=style,label=风格"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=颜色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "徽章",
		Hint:            "文本徽章",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"text":    "新品",
			"variant": "solid",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"text"},
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if p.Text == "" {
		return fmt.Errorf("徽章必须提供文字")
	}
	return nil
}

func effectiveColor(p *Props) string {
	if p.Color == "" {
		return defaultColor
	}
	return p.Color
}

//go:embed badge.css
var badgeCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	variant := p.Variant
	switch variant {
	case VariantOutline, VariantSoft:
	default:
		variant = VariantSolid
	}
	vars := map[string]string{
		"solid":   "",
		"outline": "",
		"soft":    "",
		"color":   effectiveColor(p),
	}
	vars[variant] = "1"
	if err := core.ApplyComponentCSSTmpl(b, sel, badgeCSS, vars); err != nil {

		panic(fmt.Sprintf("badge 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("badge", badgeTemplate)
}

//go:embed badge.jet
var badgeTemplate string
