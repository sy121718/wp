package richtext

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	xhtml "golang.org/x/net/html"
)

const MaxRichLen = 30000

var allowedRichTags = map[string]bool{
	"p": true, "br": true,
	"strong": true, "b": true, "em": true, "i": true,
	"u": true, "s": true, "del": true,
	"code": true, "pre": true,
	"ul": true, "ol": true, "li": true,
	"blockquote": true,
	"a":          true,
	"h1":         true, "h2": true, "h3": true, "h4": true, "h5": true,
	"hr":  true,
	"img": true, "figure": true, "figcaption": true,

	"table": true, "thead": true, "tbody": true, "tfoot": true,
	"tr": true, "th": true, "td": true, "caption": true,
	"details": true, "summary": true,

	"div": true,
}

var allowedLinkRel = map[string]bool{
	"nofollow": true, "noreferrer": true, "noopener": true,
}

var richDimensionRe = regexp.MustCompile(`^[0-9]+(%|px|em|rem|vw|vh)?$`)

var richParagraphBreakRe = regexp.MustCompile(`\n[ \t]*\n+`)

func SanitizeRichHTML(src string) string {
	if src == "" || len(src) > MaxRichLen {
		return ""
	}
	z := xhtml.NewTokenizer(strings.NewReader(src))
	var out strings.Builder

	inP := false

	for {
		tt := z.Next()
		switch tt {
		case xhtml.ErrorToken:

			return backfillFigureAlt(out.String())
		case xhtml.TextToken:

			out.WriteString(html.EscapeString(normalizeRichCR(z.Token().Data)))
		case xhtml.StartTagToken:
			tok := z.Token()

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
				continue
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

		}
	}
}

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

func RichTextHTML(src string) string {
	if src == "" {
		return ""
	}
	if !HasRichMarkup(src) {
		return plainTextToHTML(src)
	}
	return SanitizeRichHTML(src)
}

func plainTextToHTML(src string) string {
	if len(src) > MaxRichLen {
		return ""
	}
	s := strings.ReplaceAll(src, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var out strings.Builder
	for _, block := range richParagraphBreakRe.Split(s, -1) {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		escaped := strings.ReplaceAll(html.EscapeString(block), "\n", "<br>")
		out.WriteString("<p>")
		out.WriteString(escaped)
		out.WriteString("</p>")
	}
	return out.String()
}

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

func normalizeRichTag(name string) string {
	if name == "div" {
		return "p"
	}
	return name
}

func renderAllowedAttrs(tok xhtml.Token) string {

	if tok.Data == "pre" {
		return renderPreAttrs(tok)
	}
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
		case "br", "blockquote", "p", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "hr",
			"strong", "b", "em", "i", "u", "s", "del", "code", "figure", "figcaption",
			"table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption", "details", "summary":

		}
	}
	return sb.String()
}

func isSafeRichDimension(s string) bool {
	return s != "" && richDimensionRe.MatchString(s)
}

var richHrefSchemes = map[string]bool{
	"http": true, "https": true,
	"ftp": true, "ftps": true,
	"mailto": true, "tel": true, "callto": true, "sms": true,
	"cid": true, "xmpp": true, "matrix": true,
}

// IsSafeRichHref 判断富文本链接/媒体地址的 scheme 是否在白名单内
// （http/https/ftp/ftps/mailto/tel/callto/sms/cid/xmpp/matrix，以及站内相对路径）。
func IsSafeRichHref(raw string) bool { return isSafeRichHref(raw) }

// StripHrefNoise 去掉 scheme 判定前的空白与控制字符，供调用方复用同一判据。
func StripHrefNoise(s string) string { return stripRichHrefNoise(s) }

func isSafeRichHref(raw string) bool {
	href := strings.TrimSpace(raw)
	if href == "" {
		return false
	}
	probe := stripRichHrefNoise(href)
	if probe == "" {
		return false
	}
	if strings.HasPrefix(probe, "#") || strings.HasPrefix(probe, "/") {
		return true
	}

	i := strings.IndexByte(probe, ':')
	if i <= 0 {
		return false
	}
	return richHrefSchemes[strings.ToLower(probe[:i])]
}

func normalizeRichCR(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func stripRichHrefNoise(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r <= 0x20, r == 0x7f, r == 0x180e:
			return -1
		case r > 0x7f && unicode.IsSpace(r):
			return -1
		}
		return r
	}, s)
}

var richCodeLanguageRe = regexp.MustCompile(`^[A-Za-z0-9+#-]{1,32}$`)

func renderPreAttrs(tok xhtml.Token) string {
	lang := ""
	for _, attr := range tok.Attr {
		switch strings.ToLower(attr.Key) {
		case "language":
			if v, ok := normalizeCodeLanguage(attr.Val); ok {
				lang = v
			}
		case "class":
			if v := languageClassOf(attr.Val); v != "" {
				lang = v
			}
		}
	}
	if lang == "" {
		return ""
	}
	return ` class="language-` + lang + `"`
}

func normalizeCodeLanguage(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if !richCodeLanguageRe.MatchString(v) {
		return "", false
	}
	return v, true
}

func languageClassOf(class string) string {
	for _, part := range strings.Fields(class) {
		rest, ok := strings.CutPrefix(part, "language-")
		if !ok {
			continue
		}
		if v, valid := normalizeCodeLanguage(rest); valid {
			return v
		}
	}
	return ""
}

const maxFigureAltLen = 200

func backfillFigureAlt(fragment string) string {
	if fragNoFigureAltWork(fragment) {
		return fragment
	}
	var out strings.Builder
	pos, figStart, depth := 0, -1, 0
	for i := 0; i < len(fragment); {
		lt := strings.IndexByte(fragment[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		switch {
		case strings.HasPrefix(fragment[i:], "<figure>"):
			if depth == 0 {
				figStart = i
			}
			depth++
			i += len("<figure>")
		case strings.HasPrefix(fragment[i:], "</figure>"):
			if depth > 0 {
				depth--
			}
			i += len("</figure>")
			if depth == 0 && figStart >= 0 {
				out.WriteString(fragment[pos:figStart])
				out.WriteString(fillFigureAlt(fragment[figStart:i]))
				pos = i
				figStart = -1
			}
		default:

			gt := strings.IndexByte(fragment[i:], '>')
			if gt < 0 {
				return fragment
			}
			i += gt + 1
		}
	}
	out.WriteString(fragment[pos:])
	return out.String()
}

func fragNoFigureAltWork(fragment string) bool {
	return !strings.Contains(fragment, "<figure>") ||
		!strings.Contains(fragment, "<img") ||
		!strings.Contains(fragment, "<figcaption")
}

func fillFigureAlt(fig string) string {
	const open, close = "<figcaption", "</figcaption>"
	cs := strings.Index(fig, open)
	if cs < 0 {
		return fig
	}

	inner := cs + len("<figcaption>")
	if !strings.HasPrefix(fig[cs:], "<figcaption>") {
		return fig
	}
	end := strings.Index(fig[inner:], close)
	if end < 0 {
		return fig
	}

	caption := strings.Join(strings.Fields(StripRichTags(fig[inner:inner+end])), " ")
	if caption == "" {
		return fig
	}
	caption = truncateRunes(caption, maxFigureAltLen)
	alt := escapeRichAttr(caption)

	var out strings.Builder
	i := 0
	for {
		idx := strings.Index(fig[i:], "<img")
		if idx < 0 {
			break
		}
		idx += i
		gt := strings.IndexByte(fig[idx:], '>')
		if gt < 0 {
			break
		}
		gt += idx
		tag := fig[idx : gt+1]
		out.WriteString(fig[i:idx])
		if strings.Contains(tag, ` alt="`) {
			out.WriteString(tag)
		} else {
			out.WriteString(tag[:len(tag)-1])
			out.WriteString(" alt=\"" + alt + "\">")
		}
		i = gt + 1
	}
	out.WriteString(fig[i:])
	return out.String()
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func escapeRichAttr(s string) string {
	s = normalizeRichCR(s)
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
