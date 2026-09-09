// Package seo 提供页面草稿 → SEO 评分输入的提取（依赖 builder 的 JSON 形态，不依赖内部类型）。
package seo

import (
	"encoding/json"
	"regexp"
	"strings"

	"go_wp/internal/seo/scoring"
)

var tagRe = regexp.MustCompile("<[^>]+>")

func stripTags(s string) string {
	return strings.TrimSpace(tagRe.ReplaceAllString(s, " "))
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// ScoreDocument 从页面草稿 JSON 提取输入并计算评分（只读分析，不写产物）。
// pageURL 用于 URL 相关检查（可传草稿路径）。
func ScoreDocument(doc json.RawMessage, pageURL string) (*scoring.Result, error) {
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		return nil, err
	}
	in := &scoring.Input{URL: pageURL, Locale: "zh", IsHTTPS: true}
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
	in.WordCount = len([]rune(in.BodyText))
	// 结构化数据：有标题结构的内容页视为可输出 JSON-LD（构建期注入）。
	in.HasSchema = len(in.Headings) > 0
	return scoring.Score(in, nil), nil
}

// walk 递归遍历节点树，收集标题/正文/图片/链接。
func walk(nodes []any, in *scoring.Input, body *strings.Builder) {
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		props, _ := node["props"].(map[string]any)
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
