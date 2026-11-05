package slider

import (
	"html"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	DataSlider string

	HasAutoplay bool

	Autoplay string

	Loop bool

	ShowArrows bool

	ShowDots bool

	PrevLabel string
	NextLabel string

	PrevIcon string
	NextIcon string

	SlideLabel string

	Dots []DotItem
}

type DotItem struct {
	Anchor string

	Label string
}

const (
	TextKeyPrev = "site.component.slider.prev"

	TextKeyNext = "site.component.slider.next"

	TextKeySlideLabel = "site.component.slider.slide_label"
)

const (
	textFallbackPrev       = "上一张"
	textFallbackNext       = "下一张"
	textFallbackSlideLabel = "第 %s 张"
)

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.PrevLabel, v.NextLabel = textFallbackPrev, textFallbackNext
		v.SlideLabel = textFallbackSlideLabel
		v.fillDotLabels()
		return
	}
	v.PrevLabel = text(TextKeyPrev, textFallbackPrev)
	v.NextLabel = text(TextKeyNext, textFallbackNext)
	v.SlideLabel = text(TextKeySlideLabel, textFallbackSlideLabel)
	v.fillDotLabels()
}

func (v *View) fillDotLabels() {
	for i := range v.Dots {
		v.Dots[i].Label = strings.Replace(v.SlideLabel, "%s", strconv.Itoa(i+1), 1)
	}
}

func BuildView(node *core.Node, p *Props) View {
	v := View{
		DataSlider: html.EscapeString(node.ID),
		Loop:       p.Loop,
		ShowArrows: p.ShowArrows,
		ShowDots:   p.ShowDots,
	}
	if p.Autoplay > 0 {
		v.HasAutoplay = true
		v.Autoplay = strconv.FormatFloat(p.Autoplay, 'f', -1, 64)
	}

	if p.ShowDots {
		v.Dots = make([]DotItem, 0, len(node.Children))
		for i := range node.Children {
			v.Dots = append(v.Dots, DotItem{
				Anchor: "sky-slide-" + node.ID + "-" + strconv.Itoa(i),
			})
		}
	}
	if p.ShowArrows {
		if svg, ok := core.IconSVGClass("chevron-left", "sky-slider-arrow-icon"); ok {
			v.PrevIcon = svg
		}
		if svg, ok := core.IconSVGClass("chevron-right", "sky-slider-arrow-icon"); ok {
			v.NextIcon = svg
		}
	}
	return v
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.DataSlider != "" {
		attrs = append(attrs, "data-slider")
	}
	if v.HasAutoplay {
		attrs = append(attrs, "data-autoplay")
	}
	if v.Loop {
		attrs = append(attrs, "data-loop")
	}

	attrs = append(attrs, "data-track")
	if v.ShowArrows {
		attrs = append(attrs, "data-prev", "data-next")
	}
	return attrs, nil
}
