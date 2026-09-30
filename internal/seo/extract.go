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
// pageURL 用于 URL 相关检查（可传草稿路径）；lang 决定字数/句长统计口径（SEO-001）。
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

// walk 递归遍历节点树，收集标题/正文/图片/链接。
func walk(nodes []any, in *scoring.Input, body *strings.Builder) {
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		props, _ := node["props"].(map[string]any)
		// 页面文档里的表格是**组件节点**（core.table），不是正文文本 —— 正文这边收集的是
		// 纯文本，字符串里永远看不到 "<table>"。两处判定因此各按自己的形态来。
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

// tableComponentType 页面文档里表格组件的节点类型（与 internal/builder/components/table 的 Type 同值）。
//
// 写成字面量而不是 import 那个包：seo 是**纯提取与评分**层，不该把组件实现拉进来
// （组件包会反过来依赖 builder 的注册表，一引就把依赖方向搅乱）。代价是这个常量
// 与组件 Type 有分叉风险 —— 由 internal/seo 的测试钉住它还能被识别。
const tableComponentType = "core.table"

// hasComparisonTableHTML 富文本 HTML 里是否存在表格（文章的正文形态）。
//
// 只判「有没有表」，不判「表好不好」：读懂表在比什么（列是不是决策维度、值能否对照）
// 是人的判断，评分只保证这个结构存在（见 scoring.chkComparisonTable 的注释）。
func hasComparisonTableHTML(html string) bool {
	if html == "" {
		return false
	}
	return strings.Contains(strings.ToLower(html), "<table")
}
