package seo

import (
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"

	"go_wp/pkg/seo/scoring"
)

var tagRe = regexp.MustCompile("<[^>]+>")

func stripTags(s string) string {
	return strings.TrimSpace(tagRe.ReplaceAllString(s, " "))
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func ScoreDocument(doc json.RawMessage, pageURL, lang string) (*scoring.Result, error) {
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, err
	}
	if strings.TrimSpace(lang) == "" {
		lang = "zh-CN"
	}
	in := &scoring.Input{URL: pageURL, Locale: lang, IsHTTPS: true}
	if settings, ok := root["settings"].(map[string]any); ok {
		if seoMap, ok := settings["seo"].(map[string]any); ok {
			in.Title = str(seoMap["title"])
			in.MetaDescription = str(seoMap["description"])
			in.FocusKeyword = str(seoMap["focusKeyword"])
			in.Intent = scoring.QueryIntent(str(seoMap["intent"]))
			in.HasCanonical = str(seoMap["canonical"]) != ""
			if list, ok := seoMap["secondaryKeywords"].([]any); ok {
				for _, v := range list {
					if s := str(v); s != "" {
						in.SecondaryKeywords = append(in.SecondaryKeywords, s)
					}
				}
			}
		}
	}
	nodes, _ := root["root"].([]any)
	var body strings.Builder
	walk(nodes, in, &body)
	in.BodyText = body.String()
	in.WordCount = wordCount(in.BodyText, lang)
	schemaType := ""
	if settings, ok := root["settings"].(map[string]any); ok {
		if seoMap, ok := settings["seo"].(map[string]any); ok {
			schemaType = str(seoMap["schemaType"])
		}
	}
	in.HasSchema = EvaluateSchemaPresence(in.Title, in.MetaDescription, schemaType)
	return scoring.Score(in, nil), nil
}

func walk(nodes []any, in *scoring.Input, body *strings.Builder) {
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		props, _ := node["props"].(map[string]any)

		if str(node["type"]) == tableComponentType {
			in.HasComparisonTable = true
		}
		switch str(node["type"]) {
		case "core.heading":
			lvl := 2
			switch str(props["tag"]) {
			case "h1":
				lvl = 1
			case "h3":
				lvl = 3
			case "h4":
				lvl = 4
			case "h5":
				lvl = 5
			case "h6":
				lvl = 6
			}
			text := stripTags(str(props["text"]))
			in.Headings = append(in.Headings, scoring.Heading{Level: lvl, Text: text})
			body.WriteString(text)
			body.WriteString(" ")
		case "core.text":
			body.WriteString(stripTags(str(props["text"])))
			body.WriteString(" ")
		case "core.image":
			in.Images = append(in.Images, scoring.Image{
				Src: str(props["src"]), Alt: str(props["alt"]), Kind: "content",
			})
		case "core.card":
			in.Images = append(in.Images, scoring.Image{Src: str(props["imageSrc"]), Alt: str(props["title"]), Kind: "content"})
			body.WriteString(stripTags(str(props["title"])))
			body.WriteString(" ")
			body.WriteString(stripTags(str(props["text"])))
			body.WriteString(" ")
		case "core.button":
			if str(props["action"]) == "external" {
				in.ExternalLinks++
			} else {
				in.InternalLinks++
			}
			if t := stripTags(str(props["text"])); t != "" {
				in.AnchorTexts = append(in.AnchorTexts, t)
			}
		}
		if children, ok := node["children"].([]any); ok {
			walk(children, in, body)
		}
	}
}

const tableComponentType = "core.table"

func hasComparisonTableHTML(html string) bool {
	if html == "" {
		return false
	}
	return strings.Contains(strings.ToLower(html), "<table")
}
