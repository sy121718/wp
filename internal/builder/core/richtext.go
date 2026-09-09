// Package core — 富文本（rich text）字段的构建期渲染能力。
//
// 能力来源：原 internal/builder/components/text 的 sanitize 实现（docs/02-C2 §2，
// C2 存储型 XSS 修复）。card / quote / infobox / faq 等组件的「内容字段」同样需要
// 这套白名单与清洗规则，故上移到 core（组件基座）统一承载：
//   - 白名单只有一处（allowedRichTags），组件内禁止复制；
//   - text 包保留包内私有别名转发，既有测试与调用点不变。
//
// 两种输入形态（RichTextHTML 统一入口）：
//   - 富文本（含 HTML 标签）：白名单清洗 + h1→h2 降级（SanitizeRichHTML）；
//   - 存量纯文本（无标签）：HTML 转义后按空行分段包 <p>，段内换行转 <br>
//     （绝不把纯文本当 HTML 直接输出，否则 "1 < 2 & 更多" 会被解析成标签）。
package core

import (
	"html"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

// MaxRichLen 富文本内容长度上限（超出直接判空，防畸形/滥用输入膨胀产物）。
// 与 core.text 的 ct:"richtext,maxlen=30000" 取齐：字段校验上限必须 ≥ 清洗上限，
// 否则 20000~30000 之间的内容能通过校验却被静默判空（正文丢失）。
const MaxRichLen = 30000

// allowedRichTags 富文本白名单（规范 docs/02-C2 §2 编辑器能力；编辑器为 Trix 2.x）：
// 加粗/斜体/下划线/删除线/代码（含 pre 代码块）、有序/无序列表、引用块、
// 行内超链接、段落与标题（h2~h4；h1 仅入白名单以便输出侧降级为 h2，
// 正文不得出现 H1，防 SEO 层级破坏）、图片（img，src 过协议白名单）。
//
// Trix 2.x 默认工具条实际产出：p/br/strong/em/s/a/ul/ol/li/blockquote/pre/h1/figure；
// 其余（h2~h4/b/i/u/del/code/img/figcaption）来自粘贴内容或历史数据，一并保留。
var allowedRichTags = map[string]bool{
	"p": true, "br": true,
	"strong": true, "b": true, "em": true, "i": true,
	"u": true, "s": true, "del": true,
	"code": true, "pre": true,
	"ul": true, "ol": true, "li": true,
	"blockquote": true,
	"a":          true,
	"h1":         true, "h2": true, "h3": true, "h4": true,
	"img": true, "figure": true, "figcaption": true,
	// div 仅为「归一输入」入白名单：Trix 2.x 的段落容器是 <div>，
	// 输出侧由 normalizeRichTag 统一转 <p>（否则多段剥壳后会被合并成一段）。
	"div": true,
}

// allowedLinkRel 链接 rel 白名单：nofollow / noreferrer / noopener。
var allowedLinkRel = map[string]bool{
	"nofollow": true, "noreferrer": true, "noopener": true,
}

// richDimensionRe 图片宽高白名单：纯数字或数字 + 常见 CSS 长度单位。
var richDimensionRe = regexp.MustCompile(`^[0-9]+(%|px|em|rem|vw|vh)?$`)

// richParagraphBreakRe 纯文本分段：空行（含只带空格/制表符的行）。
var richParagraphBreakRe = regexp.MustCompile(`\n[ \t]*\n+`)

// SanitizeRichHTML 富文本安全白名单清洗：
//   - 非白名单标签：剥壳保留其文本内容（脚本/事件全部剥离）；
//   - 文本节点统一 HTML 转义后输出（防止 &lt;script&gt; 等实体被 tokenizer 解码后
//     以原始文本复活为真标签，见 C2 存储型 XSS 修复）；
//   - a 标签仅保留 href（http/https/mailto/相对路径/#）、target、rel；
//   - img 标签仅保留 src（过协议白名单，拒 javascript:/data:）、alt、width/height、loading；
//   - 其余属性一律剥离；注释与声明剥离；
//   - h1 统一降级为 h2（Trix「标题」按钮输出 h1；文章正文不得出现 H1，
//     SEO 要求一页一个 H1，由页面标题承担）。
//
// 输出为语义 HTML 片段（内部仅 <p>/<ul>/<blockquote>/<pre> 等白名单结构）。
func SanitizeRichHTML(src string) string {
	if src == "" || len(src) > MaxRichLen {
		return ""
	}
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	// inP 段落嵌套保护：p 与 Trix 的 div 段落容器统一输出为 <p>，
	// 已处于段落内时再遇到 p/div 只剥壳，避免产出 <p><p>…</p></p> 非法结构。
	inP := false

	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:
			return out.String()
		case xhtml.TextToken:
			// Tokenizer 已把 &lt; 等实体解码为原始字符；此处必须重新转义，
			// 否则 &lt;script&gt; 会以真标签输出（存储型 XSS）。
			out.WriteString(html.EscapeString(z.Token().Data))
		case xhtml.StartTagToken:
			tok := z.Token()
			// p / div 统一归一为 <p>：div 是 Trix 2.x 的段落容器。
			if tok.Data == "p" || tok.Data == "div" {
				if inP {
					continue
				}
				out.WriteString("<p")
				out.WriteString(renderAllowedAttrs(tok))
				out.WriteString(">")
				inP = true
				continue
			}
			if !allowedRichTags[tok.Data] {
				continue // 非白名单标签：剥壳保留内部文本
			}
			out.WriteString("<")
			out.WriteString(normalizeRichTag(tok.Data))
			out.WriteString(renderAllowedAttrs(tok))
			out.WriteString(">")
		case xhtml.EndTagToken:
			tok := z.Token()
			if tok.Data == "p" || tok.Data == "div" {
				if inP {
					out.WriteString("</p>")
					inP = false
				}
				continue
			}
			if !allowedRichTags[tok.Data] || tok.Data == "img" {
				// 非白名单标签剥壳；img 为 void 元素，无闭合标签（防御异常输入 </img>）。
				continue
			}
			out.WriteString("</")
			out.WriteString(normalizeRichTag(tok.Data))
			out.WriteString(">")
		case xhtml.SelfClosingTagToken:
			tok := z.Token()
			if !allowedRichTags[tok.Data] {
				continue
			}
			name := normalizeRichTag(tok.Data)
			out.WriteString("<")
			out.WriteString(name)
			out.WriteString(renderAllowedAttrs(tok))
			if name == "br" {
				out.WriteString("/>")
			} else {
				out.WriteString(">")
			}
		case xhtml.CommentToken, xhtml.DoctypeToken:
			// 注释与文档声明一律剥离。
		}
	}
}

// HasRichMarkup 判断内容是否含 HTML 标签（任意标签 token，含非白名单标签）。
// 用于区分「富文本输入」与「存量纯文本」：不含标签的输入一律走转义 + 段落包装。
func HasRichMarkup(src string) bool {
	if src == "" {
		return false
	}
	z := xhtml.NewTokenizer(strings.NewReader(src))
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return false
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken, xhtml.EndTagToken:
			return true
		}
	}
}

// RichTextHTML 富文本内容字段的统一渲染入口（构建期调用，产物直接进模板 unsafe 输出）：
//   - 含 HTML 标签：走白名单清洗（SanitizeRichHTML）；
//   - 不含标签（存量纯文本）：HTML 转义后按空行分段包 <p>，段内换行转 <br>。
//
// 返回值保证是安全 HTML 片段；调用方（组件 BuildView）直接交给模板 unsafe 输出。
func RichTextHTML(src string) string {
	if src == "" {
		return ""
	}
	if !HasRichMarkup(src) {
		return plainTextToHTML(src)
	}
	return SanitizeRichHTML(src)
}

// plainTextToHTML 存量纯文本 → 段落化 HTML：
// 先按空行分段，段内 HTML 转义后再把换行替换为 <br>（顺序不可颠倒，
// 否则 <br> 自身会被转义成可见文本）。空段落跳过。
func plainTextToHTML(src string) string {
	if len(src) > MaxRichLen {
		return "" // 与 SanitizeRichHTML 同口径：超长判空，防产物膨胀
	}
	s := strings.ReplaceAll(src, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var out strings.Builder
	for _, block := range richParagraphBreakRe.Split(s, -1) {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		// 顺序不可颠倒：先转义整段，再把换行替换为 <br>；
		// 反过来会把 <br> 自身转义成可见文本 &lt;br&gt;。
		escaped := strings.ReplaceAll(html.EscapeString(block), "\n", "<br>")
		out.WriteString("<p>")
		out.WriteString(escaped)
		out.WriteString("</p>")
	}
	return out.String()
}

// StripRichTags 提取富文本纯文本内容（摘要/SEO 等纯文本场景使用：strip 全部标签）。
func StripRichTags(src string) string {
	if src == "" {
		return ""
	}
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var out strings.Builder
	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:
			return out.String()
		case xhtml.TextToken:
			out.WriteString(z.Token().Data)
		}
	}
}

// normalizeRichTag 标签规范化：h1 降级为 h2、div 归一为 p。
//   - h1：Trix「标题」按钮输出 h1，而文章正文不得出现 H1（SEO 要求一页一个 H1，
//     由页面标题承担），故在白名单输出侧统一降级；
//   - div：Trix 2.x 的段落容器就是 <div>，若只剥壳不归一，多段内容会合并成一段；
//     Trix 自身不产生嵌套 div，粘贴进来的嵌套 div 由浏览器解析时容错修正。
func normalizeRichTag(name string) string {
	switch name {
	case "h1":
		return "h2"
	case "div":
		return "p"
	}
	return name
}

// renderAllowedAttrs 渲染标签允许属性（a 仅 href/target/rel，img 仅 src/alt/宽高/loading）。
func renderAllowedAttrs(tok xhtml.Token) string {
	var sb strings.Builder
	for _, attr := range tok.Attr {
		switch tok.Data {
		case "a":
			switch attr.Key {
			case "href":
				if isSafeRichHref(attr.Val) {
					sb.WriteString(` href="`)
					sb.WriteString(escapeRichAttr(attr.Val))
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
					sb.WriteString(escapeRichAttr(attr.Val))
					sb.WriteString(`"`)
				}
			}
		case "img":
			switch attr.Key {
			case "src":
				// src 协议白名单：http/https/mailto 或相对路径/#，拒绝 javascript:/data: 等。
				if isSafeRichHref(attr.Val) {
					sb.WriteString(` src="`)
					sb.WriteString(escapeRichAttr(attr.Val))
					sb.WriteString(`"`)
				}
			case "alt":
				sb.WriteString(` alt="`)
				sb.WriteString(escapeRichAttr(attr.Val))
				sb.WriteString(`"`)
			case "width", "height":
				// 宽高仅允许纯数字或数字+常见 CSS 单位（防属性注入）。
				if isSafeRichDimension(attr.Val) {
					sb.WriteString(" " + attr.Key + `="`)
					sb.WriteString(escapeRichAttr(attr.Val))
					sb.WriteString(`"`)
				}
			case "loading":
				if attr.Val == "lazy" || attr.Val == "eager" {
					sb.WriteString(` loading="` + attr.Val + `"`)
				}
			}
		case "br", "blockquote", "p", "ul", "ol", "li", "h1", "h2", "h3", "h4", "pre",
			"strong", "b", "em", "i", "u", "s", "del", "code", "figure", "figcaption":
			// 无属性白名单。
		}
	}
	return sb.String()
}

// isSafeRichDimension 图片宽高属性白名单校验（防属性注入）。
func isSafeRichDimension(s string) bool {
	return s != "" && richDimensionRe.MatchString(s)
}

// isSafeRichHref 链接协议白名单：http/https/mailto 或相对路径/# 锚点。
func isSafeRichHref(href string) bool {
	if strings.HasPrefix(href, "#") || strings.HasPrefix(href, "/") {
		return true // 锚点与站内相对路径
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") {
		return true
	}
	return false
}

// escapeRichAttr 属性值转义（引号等危险字符）。
func escapeRichAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
