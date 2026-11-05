package seo

import (
	"html"
	"regexp"
	"strings"

	"go_wp/pkg/seo/scoring"
)

var (
	headingRe = regexp.MustCompile(`(?is)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	imgTagRe  = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	imgSrcRe  = regexp.MustCompile(`(?is)\bsrc\s*=\s*["']([^"']*)["']`)
	imgAltRe  = regexp.MustCompile(`(?is)\balt\s*=\s*["']([^"']*)["']`)
	anchorRe  = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hrefRe    = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']*)["']`)
)

func ScoreArticle(data map[string]any, articleURL, lang string) *scoring.Result {
	body := articleField(data, "body")
	if strings.TrimSpace(lang) == "" {
		lang = "zh-CN"
	}
	in := &scoring.Input{
		URL:             articleURL,
		Locale:          lang,
		IsHTTPS:         true,
		Title:           articleField(data, "title"),
		MetaDescription: articleField(data, "excerpt"),
		FocusKeyword:    articleField(data, "focusKeyword"),
	}
	in.BodyText = articlePlainText(body)
	in.WordCount = wordCount(in.BodyText, lang)
	in.Headings = articleHeadings(body)
	in.Images = articleImages(body, articleField(data, "featuredImage"))
	in.InternalLinks, in.ExternalLinks, in.AnchorTexts = articleLinks(body)

	in.HasComparisonTable = hasComparisonTableHTML(body)

	in.HasCanonical = true
	in.HasSchema = EvaluateSchemaPresence(in.Title, in.MetaDescription, "article")
	return scoring.Score(in, nil)
}

func articleField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func articlePlainText(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	plain := stripTags(body)
	return strings.TrimSpace(html.UnescapeString(plain))
}

func articleHeadings(body string) []scoring.Heading {
	matches := headingRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]scoring.Heading, 0, len(matches))
	for _, m := range matches {
		level := int(m[1][0] - '0')
		text := strings.TrimSpace(html.UnescapeString(stripTags(m[2])))
		out = append(out, scoring.Heading{Level: level, Text: text})
	}
	return out
}

func articleImages(body, featured string) []scoring.Image {
	out := make([]scoring.Image, 0)
	for _, tag := range imgTagRe.FindAllString(body, -1) {
		out = append(out, scoring.Image{
			Src:  strings.TrimSpace(attrOf(imgSrcRe, tag)),
			Alt:  strings.TrimSpace(html.UnescapeString(attrOf(imgAltRe, tag))),
			Kind: "content",
		})
	}
	if strings.TrimSpace(featured) != "" {

		out = append(out, scoring.Image{Src: strings.TrimSpace(featured), Kind: "hero"})
	}
	return out
}

func articleLinks(body string) (internal, external int, anchors []string) {
	for _, m := range anchorRe.FindAllStringSubmatch(body, -1) {
		href := strings.TrimSpace(html.UnescapeString(attrOf(hrefRe, m[1])))
		if href == "" {

			continue
		}
		lower := strings.ToLower(href)
		switch {
		case strings.HasPrefix(lower, "mailto:"), strings.HasPrefix(lower, "tel:"):
		case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "//"):
			external++
		default:
			internal++
		}
		if text := strings.TrimSpace(html.UnescapeString(stripTags(m[2]))); text != "" {
			anchors = append(anchors, text)
		}
	}
	return internal, external, anchors
}

func attrOf(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}
