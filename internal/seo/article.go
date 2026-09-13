package seo

// article.go — CMS 文章（content 实体）→ SEO 评分输入的提取（SEO-10）。
//
// 与 extract.go 的分工：那里从 Page Document 的节点树提取（页面草稿，画布 AST），
// 这里从文章实体的扁平字段提取（contents.data：title / body / excerpt /
// featuredImage / seoTitle / seoDescription / focusKeyword）。两条路径共用同一个
// scoring 引擎 —— 规则只有一份，省得「页面评分」与「文章评分」给出两套口径。
//
// 三个刻意的取舍：
//
//  1. **SEO 标题/描述优先取 seoTitle / seoDescription**，缺了才回落 title / excerpt：
//     与运营的心智一致（专门的 SEO 字段填了就该用它），也与构建期 meta 的取法同向。
//
//  2. **hasCanonical / hasSchema 判真**：这两项由构建期注入，不由编辑者补。
//     文章详情页的产物现在带 canonical（来自实例线上路径）与 JSON-LD
//     （schemaType=article），实现在 presentation/service/presentation_seo.go。
//     —— 这里的口径随现实走：那块缺口补上之前，本文件判 false 并写了理由；
//     补上之后继续判 false 会让侧栏永远挂着两条改了也没用的「未达标」。
//
//  3. **正文按富文本处理**：body 是 Trix 输出（也可能是纯文本），
//     字数/标题结构/图片/链接全部从 HTML 里提取，而不是把标签当正文算进字数
//     （否则一篇 800 字的文章会因为标签被算成 3000 字）。

import (
	"html"
	"regexp"
	"strings"

	"go_wp/internal/seo/scoring"
)

// 正文提取用的正则。富文本是受控白名单（builder/core 的 allowedRichTags），
// 这里只做只读提取，不承担清洗职责 —— 落库前的白名单清洗在写入路径上。
var (
	headingRe = regexp.MustCompile(`(?is)<h([1-6])[^>]*>(.*?)</h[1-6]>`)
	imgTagRe  = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	imgSrcRe  = regexp.MustCompile(`(?is)\bsrc\s*=\s*["']([^"']*)["']`)
	imgAltRe  = regexp.MustCompile(`(?is)\balt\s*=\s*["']([^"']*)["']`)
	anchorRe  = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hrefRe    = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']*)["']`)
)

// ScoreArticle 计算一篇文章的 SEO 评分。
//
// data 为 contents.data 解出的字段表，articleURL 为该文章详情页的线上路径
// （拿不到就传空串 —— URL 相关检查会按「未知」判，不影响其余项）。
func ScoreArticle(data map[string]any, articleURL string) *scoring.Result {
	body := articleField(data, "body")
	in := &scoring.Input{
		URL:             articleURL,
		Locale:          "zh",
		IsHTTPS:         true,
		Title:           firstNonEmpty(articleField(data, "seoTitle"), articleField(data, "title")),
		MetaDescription: firstNonEmpty(articleField(data, "seoDescription"), articleField(data, "excerpt")),
		FocusKeyword:    articleField(data, "focusKeyword"),
	}
	in.BodyText = articlePlainText(body)
	in.WordCount = len([]rune(in.BodyText))
	in.Headings = articleHeadings(body)
	in.Images = articleImages(body, articleField(data, "featuredImage"))
	in.InternalLinks, in.ExternalLinks, in.AnchorTexts = articleLinks(body)
	// canonical 与结构化数据由构建期注入（presentation 侧的 applyEntitySEO）：
	// 发布出来的详情页一定带这两样，所以判真 —— 评分侧栏只该显示"编辑者能改的东西"，
	// 把系统保证项挂在上面只会教人忽略它。
	in.HasCanonical = true
	in.HasSchema = true
	return scoring.Score(in, nil)
}

// articleField 取一个字符串字段。
//
// 非字符串值一律按空处理：contents.data 是 JSONB，历史数据或手工写入可能给出数字
// 或对象，静默取空好过 panic / 拼出 "map[...]" 这种正文。
func articleField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return strings.TrimSpace(s)
}

// firstNonEmpty 取第一个非空值（SEO 字段回落用）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// articlePlainText 富文本正文 → 纯文本（去标签 + 还原 HTML 实体）。
//
// 实体必须还原：Trix 会把 & 写成 &amp;、中文引号写成实体，不还原会让这些字符
// 以 "&amp;" 五个字符计入字数与关键词密度，密度被系统性稀释。
func articlePlainText(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	plain := stripTags(body)
	return strings.TrimSpace(html.UnescapeString(plain))
}

// articleHeadings 提取正文里的标题结构（h1..h6 按出现顺序）。
//
// 文章的 H1 通常由详情页模板渲染（正文里的 h1 会被编辑器降级成 h2），
// 但这里不做假设：正文写了什么就是什么，缺 H1 该扣分就扣分。
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

// articleImages 正文内图片 + 封面图（封面按 hero 计，参与 alt 与体积的检查口径）。
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
		// 封面的 alt 用标题代替：卡片/详情页渲染封面时 alt 取自标题（cardstack 同口径），
		// 这里按「渲染出来长什么样」评分，而不是按「字段里存了什么」。
		out = append(out, scoring.Image{Src: strings.TrimSpace(featured), Kind: "hero"})
	}
	return out
}

// articleLinks 统计正文链接：内链 / 外链 / 锚文本。
//
// 三个口径：
//   - mailto: 与 tel: **不计入任何一类**（既不是站内页也不是外部页面）：
//     计进外链会让「联系我们」的邮箱被当成外链流失项扣分；
//   - 但它们的**锚文本照收** —— 锚文本检查看的是「链接文字是否描述性」，
//     写信/打电话同样是页面上的可点击文字，凭什么不算；
//   - 协议相对地址（//cdn.example.com）算外链：它指向的是别的域。
func articleLinks(body string) (internal, external int, anchors []string) {
	for _, m := range anchorRe.FindAllStringSubmatch(body, -1) {
		href := strings.TrimSpace(html.UnescapeString(attrOf(hrefRe, m[1])))
		if href == "" {
			// 没有 href 的 <a> 不是链接（占位锚点），整体跳过。
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

// attrOf 从标签/属性串里取第一个捕获组（未命中返回空串）。
func attrOf(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}
	return ""
}
