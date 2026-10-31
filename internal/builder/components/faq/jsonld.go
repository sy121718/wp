package faq

import (
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

var richTextBlockBreakRe = regexp.MustCompile(`(?i)</?(p|div|li|blockquote|pre|h[1-6]|br|figure|figcaption)\b[^>]*>`)

const (
	faqJSONLDOpen  = `<script type="application/ld+json">`
	faqJSONLDClose = "</script>"
)

func FAQPageJSONLD(p *Props) string {
	if p == nil {
		return ""
	}
	entity := mainEntityOf(p)
	if len(entity) == 0 {
		return ""
	}
	doc := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "FAQPage",
		"mainEntity": entity,
	}
	b, err := json.Marshal(doc)
	if err != nil {

		panic("faq 结构化数据序列化失败: " + err.Error())
	}
	return faqJSONLDOpen + string(b) + faqJSONLDClose
}

func mainEntityOf(p *Props) []map[string]any {
	out := make([]map[string]any, 0, len(p.Items))
	for _, it := range p.Items {
		question := strings.TrimSpace(it.Question)
		answer := plainAnswer(it.Answer)
		if question == "" || answer == "" {
			continue
		}
		out = append(out, map[string]any{
			"@type": "Question",
			"name":  question,
			"acceptedAnswer": map[string]any{
				"@type": "Answer",
				"text":  answer,
			},
		})
	}
	return out
}

func plainAnswer(src string) string {
	rich := core.RichTextHTML(src)
	if rich == "" {
		return ""
	}
	spaced := richTextBlockBreakRe.ReplaceAllString(rich, " ")
	return strings.Join(strings.Fields(core.StripRichTags(spaced)), " ")
}
