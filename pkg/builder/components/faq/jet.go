package faq

import (
	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type FaqItemView struct {
	Question string

	Answer string

	Open bool
}

type View struct {
	Items []FaqItemView

	ChevronIcon string

	FAQJSONLD string
}

func BuildView(p *Props) View {
	items := make([]FaqItemView, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, FaqItemView{Question: it.Question, Answer: core.RichTextHTML(it.Answer), Open: it.Open})
	}
	view := View{Items: items, FAQJSONLD: FAQPageJSONLD(p)}
	if svg, ok := core.IconSVGClass("chevron-down", "sky-faq-chevron"); ok {
		view.ChevronIcon = svg
	}
	return view
}
