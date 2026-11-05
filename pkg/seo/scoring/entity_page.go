package scoring

import "strings"

type PageKind string

const (
	KindProduct PageKind = "product"

	KindCategory PageKind = "category"

	KindBrand PageKind = "brand"
)

func ProfileFor(kind PageKind) *Profile {
	switch kind {
	case KindProduct:
		return ProductProfile()
	case KindCategory, KindBrand:
		return LandingProfile()
	default:
		return nil
	}
}

type EntityPageInput struct {
	Kind PageKind

	Name        string
	Subtitle    string
	Description string

	SEOTitle       string
	SEODescription string

	FocusKeyword      string
	SecondaryKeywords []string
	URL               string
	Slug              string
	Images            []Image
	SpecNames         []string
	ChildNames        []string

	HasCanonical bool
	HasSchema    bool
	Locale       string

	Intent QueryIntent
}

func EntityTitle(p *EntityPageInput) string {
	if p == nil {
		return ""
	}
	return PreferredTitle(p.SEOTitle, p.Name)
}

func PreferredTitle(seoTitle, fallback string) string {
	return firstNonEmpty(seoTitle, fallback)
}

func ScoreEntityPage(in *EntityPageInput) *Result {
	if in == nil {
		in = &EntityPageInput{}
	}
	return Score(entityInputOf(in), ProfileFor(in.Kind))
}

func entityInputOf(p *EntityPageInput) *Input {
	in := &Input{
		URL:    strings.TrimSpace(p.URL),
		Locale: p.Locale,

		Title:             PreferredTitle(p.SEOTitle, p.Name),
		MetaDescription:   PreferredTitle(p.SEODescription, p.Description),
		FocusKeyword:      strings.TrimSpace(p.FocusKeyword),
		SecondaryKeywords: p.SecondaryKeywords,
		Images:            p.Images,
		HasCanonical:      p.HasCanonical,
		HasSchema:         p.HasSchema,

		IsHTTPS: true,
		Intent:  intentOf(p),
	}
	in.Headings = entityHeadings(p)
	in.BodyText = entityBody(p)
	in.WordCount = textUnitLen(in.BodyText, in.Locale)
	return in
}

func intentOf(p *EntityPageInput) QueryIntent {
	if p.Intent != "" {
		return p.Intent
	}
	switch p.Kind {
	case KindProduct:
		return IntentTransactional
	case KindCategory, KindBrand:
		return IntentCommercial
	default:
		return IntentInformational
	}
}

func entityHeadings(p *EntityPageInput) []Heading {
	out := make([]Heading, 0, 1+len(p.SpecNames)+len(p.ChildNames))
	if name := strings.TrimSpace(p.Name); name != "" {
		out = append(out, Heading{Level: 1, Text: name})
	}
	for _, n := range p.SpecNames {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, Heading{Level: 2, Text: n})
		}
	}
	for _, n := range p.ChildNames {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, Heading{Level: 2, Text: n})
		}
	}
	return out
}

func entityBody(p *EntityPageInput) string {
	parts := make([]string, 0, 5+len(p.SpecNames)+len(p.ChildNames))
	for _, s := range []string{p.Name, p.Subtitle, p.Description} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	for _, n := range p.SpecNames {
		if n = strings.TrimSpace(n); n != "" {
			parts = append(parts, n)
		}
	}
	for _, n := range p.ChildNames {
		if n = strings.TrimSpace(n); n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
