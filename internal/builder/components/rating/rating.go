package rating

import (
	_ "embed"
	"fmt"
	"math"
	"strconv"

	"go_wp/internal/builder/core"
)

const Type = "core.rating"

const defaultMax = 5

const maxLimit = 10

const starPoints = "12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"

type Props struct {
	Value float64 `json:"value,omitempty" ct:"number,sec=content,label=评分值"`

	Max int `json:"max,omitempty" ct:"int,min=1,max=10,sec=content,label=满分"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "评分",
		Hint:            "星形评分",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"value": 4.5,
			"max":   5,
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
	if p.Max < 0 {
		return fmt.Errorf("评分星数不能为负: %d", p.Max)
	}
	if p.Value < 0 {
		return fmt.Errorf("评分值不能为负: %v", p.Value)
	}
	max := effectiveMax(p)
	if max > maxLimit {
		return fmt.Errorf("评分星数不能超过 %d: %d", maxLimit, max)
	}
	if p.Value > float64(max) {
		return fmt.Errorf("评分值 %v 超过上限 %d", p.Value, max)
	}
	return nil
}

func effectiveMax(p *Props) int {
	if p.Max <= 0 {
		return defaultMax
	}
	return p.Max
}

func fullCount(p *Props) int {
	n := int(math.Floor(p.Value))
	if n < 0 {
		n = 0
	}
	if m := effectiveMax(p); n > m {
		n = m
	}
	return n
}

func hasHalf(p *Props) bool {
	frac := p.Value - math.Floor(p.Value)
	return frac > 0 && fullCount(p) < effectiveMax(p)
}

//go:embed rating.css
var ratingCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, ratingCSS); err != nil {

		panic(fmt.Sprintf("rating 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("rating", ratingTemplate)
}

func ratingLabel(p *Props) string {
	return "评分 " + strconv.FormatFloat(p.Value, 'f', -1, 64) + " / " + strconv.Itoa(effectiveMax(p))
}

//go:embed rating.jet
var ratingTemplate string
