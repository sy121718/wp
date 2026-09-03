package text

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// 富文本白名单（规范 docs/02-C2 §2 编辑器能力）：
// 加粗/斜体/下划线/删除线/代码、有序/无序列表、引用块、行内超链接、
// 段落与标题（h2~h4，正文内禁止 h1 防 SEO 层级破坏）、图片（img，src 过协议白名单）。
var allowedRichTags = map[string]bool{
	"p": true, "br": true,
	"strong": true, "b": true, "em": true, "i": true,
	"u": true, "s": true, "del": true,
	"code": true,
	"ul":   true, "ol": true, "li": true,
	"blockquote": true,
	"a":          true,
	"h2":         true, "h3": true, "h4": true,
	"img": true, "figure": true, "figcaption": true,
}

// linkTargetMap 链接 rel 白名单：nofollow / noreferrer / noopener。
var allowedLinkRel = map[string]bool{
	"nofollow": true, "noreferrer": true, "noopener": true,
}

// sanitizeRichHTML 富文本安全白名单清洗：
//   - 非白名单标签：剥壳保留其文本内容（脚本/事件全部剥离）；
//   - a 标签仅保留 href（http/https/mailto/相对路径/#）、target、rel；
//   - img 标签仅保留 src（过协议白名单，拒 javascript:/data:）、alt、width/height、loading；
//   - 其余属性一律剥离；注释与声明剥离。
//
// 输出为语义 HTML 片段（内部仅 <p>/<ul>/<blockquote> 等白名单结构）。
func sanitizeRichHTML(src string) string {
	if src == "" || len(src) > maxRichLen {
		return ""
	}
	z := html.NewTokenizer(strings.NewReader(src))
	var out strings.Builder

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out.String()
		case html.TextToken:
			out.WriteString(z.Token().Data)
		case html.StartTagToken:
			tok := z.Token()
			if !allowedRichTags[tok.Data] {
				continue // 非白名单标签：剥壳保留内部文本
			}
			out.WriteString("<")
			out.WriteString(tok.Data)
			out.WriteString(renderAllowedAttrs(tok))
			out.WriteString(">")
		case html.EndTagToken:
			tok := z.Token()
			if !allowedRichTags[tok.Data] || tok.Data == "img" {
				// 非白名单标签剥壳；img 为 void 元素，无闭合标签（防御异常输入 </img>）。
				continue
			}
			out.WriteString("</")
			out.WriteString(tok.Data)
			out.WriteString(">")
		case html.SelfClosingTagToken:
			tok := z.Token()
			if !allowedRichTags[tok.Data] {
				continue
			}
			out.WriteString("<")
			out.WriteString(tok.Data)
			out.WriteString(renderAllowedAttrs(tok))
			if tok.Data == "br" {
				out.WriteString("/>")
			} else {
				out.WriteString(">")
			}
		case html.CommentToken, html.DoctypeToken:
			// 注释与文档声明一律剥离。
		}
	}
}

// renderAllowedAttrs 渲染标签允许属性（a 仅 href/target/rel）。
func renderAllowedAttrs(tok html.Token) string {
	var sb strings.Builder
	for _, attr := range tok.Attr {
		switch tok.Data {
		case "a":
			switch attr.Key {
			case "href":
				if isSafeHref(attr.Val) {
					sb.WriteString(` href="`)
					sb.WriteString(escapeAttr(attr.Val))
					sb.WriteString(`"`)
				}
			case "target":
				if attr.Val == "_blank" {
					sb.WriteString(` target="_blank"`)
				}
			case "rel":
				// rel 白名单拆分校验（防止 rel 注入其他 token）。
				ok := true
				for _, part := range strings.Fields(attr.Val) {
					if !allowedLinkRel[part] {
						ok = false
						break
					}
				}
				if ok && attr.Val != "" {
					sb.WriteString(` rel="`)
					sb.WriteString(escapeAttr(attr.Val))
					sb.WriteString(`"`)
				}
			}
		case "img":
			switch attr.Key {
			case "src":
				// src 协议白名单：http/https/mailto 或相对路径/#，拒绝 javascript:/data: 等。
				if isSafeHref(attr.Val) {
					sb.WriteString(` src="`)
					sb.WriteString(escapeAttr(attr.Val))
					sb.WriteString(`"`)
				}
			case "alt":
				sb.WriteString(` alt="`)
				sb.WriteString(escapeAttr(attr.Val))
				sb.WriteString(`"`)
			case "width", "height":
				// 宽高仅允许纯数字或数字+常见 CSS 单位（防属性注入）。
				if isSafeDimension(attr.Val) {
					sb.WriteString(" " + attr.Key + `="`)
					sb.WriteString(escapeAttr(attr.Val))
					sb.WriteString(`"`)
				}
			case "loading":
				if attr.Val == "lazy" || attr.Val == "eager" {
					sb.WriteString(` loading="` + attr.Val + `"`)
				}
			}
		case "br", "blockquote", "p", "ul", "ol", "li", "h2", "h3", "h4", "strong", "b",
			"em", "i", "u", "s", "del", "code", "figure", "figcaption":
			// 无属性白名单。
		}
	}
	return sb.String()
}

// dimensionRe 图片宽高白名单：纯数字或数字 + 常见 CSS 长度单位。
var dimensionRe = regexp.MustCompile(`^[0-9]+(%|px|em|rem|vw|vh)?$`)

// isSafeDimension 图片宽高属性白名单校验（防属性注入）。
func isSafeDimension(s string) bool {
	return s != "" && dimensionRe.MatchString(s)
}

// isSafeHref 链接协议白名单：http/https/mailto 或相对路径/# 锚点。
func isSafeHref(href string) bool {
	if strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/") {
		return true // 锚点与站内相对路径
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") {
		return true
	}
	return false
}

// escapeAttr 属性值转义（引号等危险字符）。
func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// stripRichTags 提取富文本纯文本内容（Excerpt 摘要模式使用：strip 全部标签）。
func stripRichTags(src string) string {
	if src == "" {
		return ""
	}
	z := html.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out.String()
		case html.TextToken:
			out.WriteString(z.Token().Data)
		}
	}
}

// maxRichLen 富文本内容长度上限。
const maxRichLen = 20000
