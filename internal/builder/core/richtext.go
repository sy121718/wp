// Package core — 富文本（rich text）字段的构建期渲染能力。
//
// 能力来源：原 internal/builder/components/text 的 sanitize 实现（docs/02-C2 §2，
// C2 存储型 XSS 修复）。card / quote / infobox / faq 等组件的「内容字段」同样需要
// 这套白名单与清洗规则，故上移到 core（组件基座）统一承载：
//   - 白名单只有一处（allowedRichTags），组件内禁止复制；
//   - text 包保留包内私有别名转发，既有测试与调用点不变。
//
// 两种输入形态（RichTextHTML 统一入口）：
//   - 富文本（含 HTML 标签）：白名单清洗（SanitizeRichHTML）；
//   - 存量纯文本（无标签）：HTML 转义后按空行分段包 <p>，段内换行转 <br>
//     （绝不把纯文本当 HTML 直接输出，否则 "1 < 2 & 更多" 会被解析成标签）。
package core

import (
	"html"
	"regexp"
	"strings"
	"unicode"

	xhtml "golang.org/x/net/html"
)

// MaxRichLen 富文本内容长度上限（超出直接判空，防畸形/滥用输入膨胀产物）。
// 与 core.text 的 ct:"richtext,maxlen=30000" 取齐：字段校验上限必须 ≥ 清洗上限，
// 否则 20000~30000 之间的内容能通过校验却被静默判空（正文丢失）。
const MaxRichLen = 30000

// allowedRichTags 富文本白名单（规范 docs/02-C2 §2 编辑器能力）。
//
// 编辑器是 Trix 2.x + 本项目在它之上的 rich-editor 扩展
// （internal/templates/static/js/rich-editor/，统一片段 admin/partials/rich_editor.html）：
//   - 行内：加粗 / 斜体 / 下划线 / 删除线 / 代码 / 行内超链接；
//   - 块级：段落（p）、标题 h1~h5、水平线（hr）、有序 / 无序列表、引用块、代码块（pre）；
//   - 表格：table / thead / tbody / tfoot / tr / th / td / caption；
//   - 折叠块：details / summary；
//   - 图片：img（src 过协议白名单）与 figure / figcaption。
//
// 明确禁止（既不在标签白名单、也不在属性白名单里）：script / style / iframe / object /
// embed / form 等可执行或可提交的标签，以及一切 on* 事件属性。非白名单标签一律
// 「剥壳保留文本」，文本节点还会重新 HTML 转义 —— 因此剥壳后的脚本只可能以字面文本出现，
// 不可能执行（回归用例见 text 包的 TestSanitizeRichHTMLScriptStripped）。
//
// Trix 2.x 默认工具条实际产出：p/br/strong/em/s/a/ul/ol/li/blockquote/pre/h1/figure；
// 其余（h2~h5/b/i/u/del/code/img/figcaption/表格/折叠块）来自 rich-editor 扩展、
// 粘贴内容或历史数据，一并保留。
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
	// 表格与折叠块：rich-editor 扩展的块级能力。编辑器里它们是原子附件，
	// 提交时由前端展开成真标签（见 static/js/rich-editor/index.js 的 toDocumentHTML），
	// 所以服务端白名单认识的是这里的真标签。
	"table": true, "thead": true, "tbody": true, "tfoot": true,
	"tr": true, "th": true, "td": true, "caption": true,
	"details": true, "summary": true,
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
//   - a 标签仅保留 href（协议白名单与 Trix 的 URI 白名单取齐，见 richHrefSchemes）、target、rel；
//   - img 标签仅保留 src（过协议白名单，拒 javascript:/data:）、alt、width/height、loading；
//     figure 内的 img 若没有 alt，会用同级 figcaption 的纯文本回填（SEO 图片检查要的 alt）；
//   - pre 标签仅保留代码语言：Trix 的 language 属性与既有的 class="language-xxx" 归一为同一个
//     class="language-<值>"（值过 richCodeLanguageRe，非法则整个属性丢弃）；
//   - 其余属性一律剥离（含一切 on* 事件属性）；注释与声明剥离；
//   - h1~h5 原样保留：本轮产品要求编辑器支持 h1~h5，清洗器不再改写标题级别
//     （此前 h1 统一降级为 h2；一页一个 H1 改由作者与页面标题共同负责）。
//
// 输出为语义 HTML 片段（内部仅 <p>/<ul>/<blockquote>/<pre>/<table>/<details> 等白名单结构）。
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
			// 图片 alt 回填必须在**输出串**上做（需要 figcaption 的文本，它可能出现在 img 之后），
			// 而不是在流式循环里做 —— 循环只有一个前进方向，没有回看能力。
			return backfillFigureAlt(out.String())
		case xhtml.TextToken:
			// Tokenizer 已把 &lt; 等实体解码为原始字符；此处必须重新转义，
			// 否则 &lt;script&gt; 会以真标签输出（存储型 XSS）。
			out.WriteString(html.EscapeString(normalizeRichCR(z.Token().Data)))
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

// normalizeRichTag 标签规范化：div 归一为 p。
//   - div：Trix 2.x 的段落容器就是 <div>，若只剥壳不归一，多段内容会合并成一段；
//     Trix 自身不产生嵌套 div，粘贴进来的嵌套 div 由浏览器解析时容错修正。
//   - 标题不再降级：h1~h5 由编辑器工具条直接产出（rich-editor/block-level.js），
//     白名单原样保留级别 —— 曾经的 h1→h2 降级会让「编辑器里点的 H1」在保存后变成 H2。
func normalizeRichTag(name string) string {
	if name == "div" {
		return "p"
	}
	return name
}

// renderAllowedAttrs 渲染标签允许属性（a 仅 href/target/rel，img 仅 src/alt/宽高/loading，
// pre 仅代码语言）。
func renderAllowedAttrs(tok xhtml.Token) string {
	// pre 单独一条路径：它允许的两种写法（language 属性 / class="language-xxx"）必须收敛成
	// **一个** class 属性 —— 逐属性走下面的 switch 会产出两个 class（HTML 里后一个覆盖前一个，
	// 语言就随机丢了）。
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
				// src 与 href 共用同一份协议白名单（richHrefSchemes）：拒绝 javascript:/data: 等。
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
		case "br", "blockquote", "p", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "hr",
			"strong", "b", "em", "i", "u", "s", "del", "code", "figure", "figcaption",
			"table", "thead", "tbody", "tfoot", "tr", "th", "td", "caption", "details", "summary":
			// 无属性白名单：表格不给 colspan/rowspan、折叠块不给 open ——
			// 这些属性目前编辑器用不到，少一个属性就少一个注入面。
		}
	}
	return sb.String()
}

// isSafeRichDimension 图片宽高属性白名单校验（防属性注入）。
func isSafeRichDimension(s string) bool {
	return s != "" && richDimensionRe.MatchString(s)
}

// richHrefSchemes 链接协议白名单：与 Trix 自身的 URI 白名单取齐。
//
// 依据是 vendor 的 trix.umd.js（IS_ALLOWED_URI）：
//
//	^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp|matrix):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i
//
// 也就是 ftp / ftps / http / https / mailto / tel / callto / sms / cid / xmpp / matrix。
// 在此之前这里只放行 http/https/mailto —— 运营在编辑器里插的电话（tel:）与短信（sms:）
// 链接保存后 href 会被静默丢掉，<a> 退化成纯文本，而编辑器里看起来一切正常。
//
// **显式列举**而不是照抄那条正则：正则里的 [^a-z] 分支会顺带放行任何「首字符非字母」的写法，
// 那种「因为没匹配上协议所以放行」的判定迟早被 javascript:/data:/vbscript: 绕过
// （它们的首字符都是字母，但只要有别的写法钻进那条分支就是洞）。这里只认列出的协议，
// 没列出的（含 data / javascript / vbscript / blob / file）一律不放行。
var richHrefSchemes = map[string]bool{
	"http": true, "https": true,
	"ftp": true, "ftps": true,
	"mailto": true, "tel": true, "callto": true, "sms": true,
	"cid": true, "xmpp": true, "matrix": true,
}

// isSafeRichHref 链接协议白名单：richHrefSchemes 里的协议，或相对路径 / # 锚点。
//
// 判定前先剥掉全部空白与控制字符（Trix 的 ATTR_WHITESPACE 同一手法）：
// " javascript:alert(1)" / "java\tscript:alert(1)" 这类前导空白与制表符绕过，
// 不剥就会让 " javascript:" 落在「没匹配上任何协议」的分支里 —— 看起来结果一样（都被丢），
// 但那只是恰好：判定一旦改成 contains 或前缀表补全，洞立刻出现。剥掉后再判，结论与浏览器
// 解析 URL 的口径一致（浏览器同样忽略前导空白）。
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
		return true // 锚点与站内相对路径
	}
	// 无协议（相对路径）保持既有口径不放行：本轮只对齐协议集合，
	// 放宽相对路径判定是另一件事（会影响存量数据里的裸文件名 src）。
	i := strings.IndexByte(probe, ':')
	if i <= 0 {
		return false
	}
	return richHrefSchemes[strings.ToLower(probe[:i])]
}

// normalizeRichCR 换行归一：HTML 的 tokenizer（与浏览器）把 CRLF 和孤立 CR 一律当 LF，
// 而清洗时若把 CR 原样写出，第二次清洗就会得到 LF —— 输出于是不幂等
// （消毒 fuzz 抓到过：sanitize("&#13") = "\r"，再消毒得 "\n"）。归一放在写出之前，
// 让"我们产出的字节"与"解析器读到的字节"是同一套换行口径。
func normalizeRichCR(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// stripRichHrefNoise 剥掉协议判定用的空白与控制字符（含各类 Unicode 空格）。
//
// 与 Trix/DOMPurify 的 ATTR_WHITESPACE 同口径：判协议之前必须先去掉这些字符，
// 否则 "\u0000javascript:" / "\u3000javascript:" 这类绕过只是"碰巧"落在拒绝分支里。
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

// richCodeLanguageRe 代码块语言值白名单：字母数字与 + # -（C++ / C# / objective-c 这类写法），
// 长度 1~32。带引号、空格、尖括号、超长的值一律判非法 —— 判非法时整个属性丢弃，
// 绝不"截断后再放行"（截断会把 <script> 截成 script 之类的值放出去）。
var richCodeLanguageRe = regexp.MustCompile(`^[A-Za-z0-9+#-]{1,32}$`)

// renderPreAttrs <pre> 的属性渲染：只认代码语言，且**归一**成 class="language-<值>"。
//
// 来源有两条，都归一到同一个输出形态：
//   - Trix 的代码块声明 htmlAttributes: ["language"]，序列化成 <pre language="go">；
//   - 外部 HTML 与历史数据里已经写好的 <pre class="language-xxx">。
//
// 输出最多一个 class 属性（language 优先于 class），其余属性（含 on* 事件）一律丢弃。
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

// normalizeCodeLanguage 语言值校验与归一：非法返回 ok=false（调用方丢弃整个属性）。
func normalizeCodeLanguage(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if !richCodeLanguageRe.MatchString(v) {
		return "", false
	}
	return v, true
}

// languageClassOf 从 class 列表里取 language-xxx 的取值；其余类名忽略，
// 但取值本身仍要过语言白名单（class="language-<script>" 同样判非法）。
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

// maxFigureAltLen figure 图注回填成 alt 时的长度上限（字符数）。
//
// 图注是给人看的短句，超过这个长度就不再是图注而是正文 —— 不截断地整段写进 alt
// 会把属性撑成一条正文，既没有 SEO 价值也让产物变丑。超长时按字符截断（不是丢弃：
// 图注本身留在 figcaption 里，alt 只是它的摘要）。
const maxFigureAltLen = 200

// backfillFigureAlt 把 <figure> 内 figcaption 的纯文本回填为同级 <img> 的 alt。
//
// 为什么需要：Trix 的图片预览附件只渲染 <img src>，图注落在 <figcaption> 里，
// 于是正文图片一律没有 alt —— SEO 图片检查扣分，读屏软件也只能念出文件名。
// 图注本来就是这张图的描述，回填是零信息损失的补充（不改变 figcaption 本身）。
//
// 行为边界（三条都要成立才回填）：
//   - 只处理 <figure> 内部（同级的 figcaption 才有指向意义，页面别处的图注不是这张图的）；
//   - img 必须**没有 alt**：作者显式写过的 alt（含 alt="" 的装饰图声明）绝不覆盖；
//   - figcaption 的纯文本去掉标签与首尾空白后不能为空。
//
// 输入是 SanitizeRichHTML 自己的输出（标签形态固定：无属性的 <figure> / <figcaption>，
// 属性值里的引号与尖括号都已转义），因此这里按字节扫描是安全的 —— 不会把属性值里的
// 文本误认成标签（属性值里不可能出现裸的 < 或 "）。
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
			// 跳过整个标签，避免把 <figurex> 这类自定义标签名当成 <figure>。
			gt := strings.IndexByte(fragment[i:], '>')
			if gt < 0 {
				return fragment // 畸形残片：原样返回，宁可少回填也不改写输入
			}
			i += gt + 1
		}
	}
	out.WriteString(fragment[pos:])
	return out.String()
}

// fragNoFigureAltWork 快速短路：没有 figure、没有 img、或没有 figcaption 时无需扫描。
// 富文本字段是构建期热路径（每篇文章每个正文块都过一次），白跑一遍全串扫描不值当。
func fragNoFigureAltWork(fragment string) bool {
	return !strings.Contains(fragment, "<figure>") ||
		!strings.Contains(fragment, "<img") ||
		!strings.Contains(fragment, "<figcaption")
}

// fillFigureAlt 回填单个 <figure> 片段里所有缺 alt 的 img。
func fillFigureAlt(fig string) string {
	const open, close = "<figcaption", "</figcaption>"
	cs := strings.Index(fig, open)
	if cs < 0 {
		return fig
	}
	// <figcaption> 没有属性白名单，形态固定为 "<figcaption>"。
	inner := cs + len("<figcaption>")
	if !strings.HasPrefix(fig[cs:], "<figcaption>") {
		return fig
	}
	end := strings.Index(fig[inner:], close)
	if end < 0 {
		return fig
	}
	// 去标签 + 去首尾空白：图注里可能有 <strong>、<a> 等行内格式，alt 只要纯文本。
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
			out.WriteString(tag) // 已有 alt：一个字都不动
		} else {
			out.WriteString(tag[:len(tag)-1])
			out.WriteString(" alt=\"" + alt + "\">")
		}
		i = gt + 1
	}
	out.WriteString(fig[i:])
	return out.String()
}

// truncateRunes 按字符数截断（不切碎多字节字符）。
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// escapeRichAttr 属性值转义（引号等危险字符）。
func escapeRichAttr(s string) string {
	s = normalizeRichCR(s)
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
