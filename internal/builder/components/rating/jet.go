package rating

import (
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

const (
	starFormFull  = "full"
	starFormHalf  = "half"
	starFormEmpty = "empty"
)

type StarView struct {
	Form string

	Points string
}

type View struct {
	Stars []StarView

	Label string

	value float64
	max   int
}

const TextKeyLabel = "site.component.rating.label"

const textFallbackLabel = "评分 %s / %s"

func BuildView(p *Props) View {
	max := effectiveMax(p)
	full := fullCount(p)
	half := hasHalf(p)

	stars := make([]StarView, 0, max)
	for i := range max {
		var form string
		switch {
		case i < full:
			form = starFormFull
		case i == full && half:
			form = starFormHalf
		default:
			form = starFormEmpty
		}
		stars = append(stars, StarView{Form: form, Points: starPoints})
	}
	return View{Stars: stars, Label: ratingLabel(p), value: p.Value, max: effectiveMax(p)}
}

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	tpl := textFallbackLabel
	if text != nil {
		tpl = text(TextKeyLabel, textFallbackLabel)
	}
	value := strconv.FormatFloat(v.value, 'f', -1, 64)
	max := strconv.Itoa(v.max)
	switch n := strings.Count(tpl, "%s"); {
	case n >= 2:
		v.Label = fmt.Sprintf(tpl, value, max)
	case n == 1:
		v.Label = fmt.Sprintf(tpl, value)
	default:
		v.Label = tpl
	}
}
