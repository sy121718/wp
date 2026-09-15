package builder

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"strings"
)

// seo_head.go — 构建期 SEO 头输出（canonical / OG / Twitter / JSON-LD）。
//
// 三级默认回落（docs/06-A §2.1）：页面 settings.seo → 站点默认 → 省略。
// 全部内容参与确定性构建：同一输入产生相同字节。

// Alternate 该页面的另一种语言版本（多语言 P3，docs/06-D §5）。
// Href 为可直接引用的 URL（配置了站点基址时为绝对 URL，否则为站点内路径）；
// Default=true 的那条同时输出 hreflang="x-default"。
type Alternate struct {
	Lang    string
	Href    string
	Default bool
}

// robots 指令取值（页面设置的 robotsIndex / robotsFollow 白名单）。
const (
	robotIndex    = "index"
	robotNoIndex  = "noindex"
	robotFollow   = "follow"
	robotNoFollow = "nofollow"
)

// robotsContent 组装 robots 指令：两项都取默认（index/follow）时返回空串 ——
// 默认页面的产物字节因此与「没有这个功能」时完全一致（确定性构建不变量）。
func robotsContent(index, follow string) string {
	idx := strings.ToLower(strings.TrimSpace(index))
	fol := strings.ToLower(strings.TrimSpace(follow))
	if idx == "" {
		idx = robotIndex
	}
	if fol == "" {
		fol = robotFollow
	}
	if idx == robotIndex && fol == robotFollow {
		return ""
	}
	return idx + "," + fol
}

// ogType Open Graph 类型：跟随结构化数据类型 —— 社交平台据此判断内容形态，
// 固定输出 website 会让文章/商品在分享卡片里丢掉类型信息。
func ogType(schemaType string) string {
	switch strings.ToLower(strings.TrimSpace(schemaType)) {
	case "article":
		return "article"
	case "product":
		return "product"
	}
	return "website"
}

// BuildSEOHead 生成注入 <head> 的 SEO 片段（canonical / OG / Twitter / JSON-LD / hreflang）。
// pageURL 为页面最终 URL（空则省略 URL 相关标签）。
// breadcrumbHome 为 JSON-LD 面包屑首项文案（空则回退 "Home"）。
// alternates 为同页其他语言版本（空 = 单语言站点，输出与 P3 之前逐字节一致）；
// 至少两种语言时才输出 hreflang（自指单条无意义）。
func BuildSEOHead(seo SEO, pageURL, title, description, breadcrumbHome string, alternates []Alternate) string {
	if title == "" {
		title = seo.Title
	}
	if description == "" {
		description = seo.Description
	}
	var sb strings.Builder
	esc := html.EscapeString

	canonical := canonicalPublicPath(strings.TrimSpace(seo.Canonical))
	if canonical == "" {
		canonical = canonicalPublicPath(strings.TrimSpace(pageURL))
	}
	if canonical != "" {
		fmt.Fprintf(&sb, "<link rel=\"canonical\" href=\"%s\">\n", esc(canonical))
	}
	pubURL := canonicalPublicPath(strings.TrimSpace(pageURL))
	if pubURL == "" {
		pubURL = canonical
	}
	if rb := robotsContent(seo.RobotsIndex, seo.RobotsFollow); rb != "" {
		fmt.Fprintf(&sb, "<meta name=\"robots\" content=\"%s\">\n", esc(rb))
	}
	if title != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:title\" content=\"%s\">\n", esc(title))
		fmt.Fprintf(&sb, "<meta name=\"twitter:title\" content=\"%s\">\n", esc(title))
	}
	if description != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:description\" content=\"%s\">\n", esc(description))
		fmt.Fprintf(&sb, "<meta name=\"twitter:description\" content=\"%s\">\n", esc(description))
	}
	if pubURL != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:url\" content=\"%s\">\n", esc(pubURL))
	}
	fmt.Fprintf(&sb, "<meta property=\"og:type\" content=\"%s\">\n", ogType(seo.SchemaType))
	if seo.OGImage != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:image\" content=\"%s\">\n", esc(seo.OGImage))
		fmt.Fprintf(&sb, "<meta name=\"twitter:image\" content=\"%s\">\n", esc(seo.OGImage))
		fmt.Fprint(&sb, "<meta name=\"twitter:card\" content=\"summary_large_image\">\n")
	} else {
		fmt.Fprint(&sb, "<meta name=\"twitter:card\" content=\"summary\">\n")
	}
	if ld := buildJSONLD(canonical, title, description, seo.OGImage, seo.SchemaType, seo.ProductOffer, breadcrumbHome); ld != "" {
		sb.WriteString(ld)
		sb.WriteString("\n")
	}
	sb.WriteString(alternateLinks(normalizeAlternates(alternates)))
	return strings.TrimRight(sb.String(), "\n")
}

func canonicalPublicPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "/index" || path == "/index.html" {
		return "/"
	}
	if strings.HasSuffix(path, "/index") {
		parent := strings.TrimSuffix(path, "/index")
		if parent == "" {
			return "/"
		}
		return parent
	}
	if strings.HasSuffix(path, "/index.html") {
		parent := strings.TrimSuffix(path, "/index.html")
		if parent == "" {
			return "/"
		}
		return parent
	}
	return path
}

func normalizeAlternates(alternates []Alternate) []Alternate {
	if len(alternates) == 0 {
		return nil
	}
	out := make([]Alternate, 0, len(alternates))
	for _, a := range alternates {
		a.Href = canonicalPublicPath(a.Href)
		out = append(out, a)
	}
	return out
}

// alternateLinks 生成 hreflang 互指标签（语言码升序，x-default 固定最后）。
// 少于两种有效语言时返回空串——单语言站点产物字节保持不变（确定性构建不变量）。
func alternateLinks(alternates []Alternate) string {
	valid := make([]Alternate, 0, len(alternates))
	for _, a := range alternates {
		if strings.TrimSpace(a.Lang) == "" || strings.TrimSpace(a.Href) == "" {
			continue
		}
		valid = append(valid, a)
	}
	if len(valid) < 2 {
		return ""
	}
	// 稳定排序：语言码升序（插入排序，避免引入 sort 依赖差异）。
	main := make([]Alternate, 0, len(valid))
	xdefault := ""
	for _, a := range valid {
		if a.Default && xdefault == "" {
			xdefault = a.Href
		}
		main = append(main, a)
	}
	for i := 1; i < len(main); i++ {
		for j := i; j > 0 && main[j].Lang < main[j-1].Lang; j-- {
			main[j], main[j-1] = main[j-1], main[j]
		}
	}
	var sb strings.Builder
	esc := html.EscapeString
	for _, a := range main {
		fmt.Fprintf(&sb, "<link rel=\"alternate\" hreflang=\"%s\" href=\"%s\">\n", esc(a.Lang), esc(a.Href))
	}
	if xdefault != "" {
		fmt.Fprintf(&sb, "<link rel=\"alternate\" hreflang=\"x-default\" href=\"%s\">\n", esc(xdefault))
	}
	return sb.String()
}

// schemaOrgContext 结构化数据的统一 @context（页面级与站点级共用）。
//
// 写成常量而不是到处重复字面量：同一个字符串拼错一处，那一份 JSON-LD 就整份失效，
// 而且失败是静默的（页面看着正常）。
const schemaOrgContext = "https://schema.org"

// siteBaseURLEnv 站点公开根地址的环境变量名。
//
// 与 sitemap / robots.txt（internal/module/page/service/page_publish_url.go）、
// 语言切换链接（internal/pipeline/site_lang.go）读的是同一个变量：结构化数据里的
// 站点 url 必须与 sitemap 的 <loc> 指向同一域名，否则站点对外宣称了两个实体。
// 变量可带路径前缀（开发环境就是 http://127.0.0.1:8080/site），这里保留前缀。
const siteBaseURLEnv = "WP_SITE_BASE_URL"

// siteSearchQueryParam 站内搜索的关键词参数名。
//
// 与 core.searchResults 组件的搜索表单同源（search_widget.jet 的 <input name="q">，
// 表单 GET 提交到所在页面）：SearchAction 的 urlTemplate 必须用同一个参数名，
// 否则 Google 搜索结果里的站内搜索框点开是个参数被忽略的页面。
const siteSearchQueryParam = "q"

// siteRoot 站点根 URL（无尾斜杠）；无法确定时返回空串。
//
// 两级来源：部署环境变量优先（与 sitemap 同源、含路径前缀）；否则从页面的绝对
// canonical 里取 origin —— 只配了站内相对路径的站点推不出域名，此时不输出站点级
// 结构化数据（宁可不输出，也不要宣称一个错的站点地址）。
func siteRoot(canonical string) string {
	if base := strings.TrimSpace(os.Getenv(siteBaseURLEnv)); base != "" {
		if u, err := url.Parse(base); err == nil && u.Scheme != "" && u.Host != "" {
			return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
		}
	}
	u, err := url.Parse(strings.TrimSpace(canonical))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// isHomeCanonical 判断 canonical 是否站点首页（路径为空或 "/"）。
func isHomeCanonical(canonical string) bool {
	c := strings.TrimSpace(canonical)
	if c == "" {
		return false
	}
	if strings.HasPrefix(c, "/") {
		return c == "/"
	}
	u, err := url.Parse(c)
	if err != nil || u.Opaque != "" {
		return false
	}
	return u.Path == "" || u.Path == "/"
}

// homeSiteRoot 首页的站点根（含尾斜杠）；非首页、站点名缺失或站点根未知时返回空串。
//
// 站点名是 Organization 的必需属性，站点根是 WebSite 的必需属性 —— 任一缺失都不输出，
// 因为「不完整的结构化数据」在 Search Console 里是报错，而不是降级。
func homeSiteRoot(canonical, name string) string {
	if strings.TrimSpace(name) == "" || !isHomeCanonical(canonical) {
		return ""
	}
	root := siteRoot(canonical)
	if root == "" {
		return ""
	}
	return root + "/"
}

// organizationNode 品牌实体（Organization）：Google 知识面板的输入。
func organizationNode(home, name, logo string) map[string]any {
	node := map[string]any{
		"@type": "Organization",
		"@id":   home + "#organization",
		"name":  name,
		"url":   home,
	}
	if logo != "" {
		node["logo"] = logo
	}
	return node
}

// websiteNode 站点实体（WebSite + 站内搜索动作）。
//
// potentialAction 是 Google 在搜索结果里展示站内搜索框的依据；target 用
// EntryPoint/urlTemplate 形态，关键词占位符必须与站内搜索组件的参数名一致。
func websiteNode(home, name string) map[string]any {
	return map[string]any{
		"@type": "WebSite",
		"@id":   home + "#website",
		"name":  name,
		"url":   home,
		"potentialAction": map[string]any{
			"@type": "SearchAction",
			"target": map[string]any{
				"@type":       "EntryPoint",
				"urlTemplate": home + "?" + siteSearchQueryParam + "={search_term_string}",
			},
			"query-input": "required name=search_term_string",
		},
	}
}

// schemaTypeMap 结构化数据类型映射（空 = WebPage）。
//
// 刻意没有 "faq" → FAQPage 这一项：FAQPage 的产出责任在 core.faq 组件
// （internal/builder/components/faq/jsonld.go 输出带 mainEntity 的那份，与可见问答同源）。
// 页面设置里选了 faq 时这里再输出一次，页面上就会有两个 FAQPage 节点 —— 其中一个是没内容的
// 空壳，属于 Search Console 里的「无效结构化数据」，对整站数据质量是负收益（审计 SEO-006）。
// 因此 schemaType=faq 的页面仍按默认的 WebPage 输出页面级主实体。
var schemaTypeMap = map[string]string{
	"website": "WebSite", "article": "Article", "product": "Product",
}

// buildJSONLD 生成结构化数据（JSON-LD）：按页面类型输出主实体 + 面包屑。
func buildJSONLD(url, title, description, image, schemaType string, offer *ProductOfferLD, breadcrumbHome string) string {
	if title == "" && description == "" {
		return ""
	}
	typeName := "WebPage"
	if v, ok := schemaTypeMap[strings.ToLower(strings.TrimSpace(schemaType))]; ok {
		typeName = v
	}
	doc := map[string]any{
		"@context": schemaOrgContext,
		"@type":    typeName,
	}
	if title != "" {
		doc["name"] = title
		doc["headline"] = title
	}
	if description != "" {
		doc["description"] = description
	}
	if url != "" {
		doc["url"] = url
	}
	if image != "" {
		doc["image"] = image
	}
	if offer != nil && strings.EqualFold(schemaType, "product") {
		if offer.SKU != "" {
			doc["sku"] = offer.SKU
		}
		if offer.Price != "" {
			doc["offers"] = map[string]any{
				"@type":         "Offer",
				"price":         offer.Price,
				"priceCurrency": seoDefaultString(offer.PriceCurrency, "CNY"),
				"availability":  "https://schema.org/" + seoDefaultString(offer.Availability, "InStock"),
				"url":           url,
			}
		}
		if offer.RatingCount > 0 && offer.RatingValue > 0 {
			doc["aggregateRating"] = map[string]any{
				"@type":       "AggregateRating",
				"ratingValue": offer.RatingValue,
				"reviewCount": offer.RatingCount,
			}
		}
	}
	if crumbs := breadcrumbList(url, breadcrumbHome); len(crumbs) > 1 {
		doc["breadcrumb"] = map[string]any{
			"@type":           "BreadcrumbList",
			"itemListElement": crumbs,
		}
	}
	// 站点级结构化数据（审计 SEO-007）：首页追加 Organization 与 WebSite（含站内搜索）。
	//
	// 只在首页输出：品牌与站内搜索入口是站点级事实，每页重复只是噪声。
	// 站点级节点与页面级主实体放进同一个 @graph、而不是另起一个 <script>：
	// 页面上多份 JSON-LD 各自是独立断言，同类型节点重复时搜索引擎只能猜哪份是真的。
	// 站点根或站点名缺失时不输出：Organization 缺 name 属无效结构化数据。
	if home := homeSiteRoot(url, title); home != "" {
		graph := make([]any, 0, 3)
		graph = append(graph, organizationNode(home, title, image))
		if typeName == "WebSite" {
			// 页面主实体本身就是 WebSite（首页显式设了 schemaType=website）：
			// 站点级字段合并进它，而不是再追加一个同类型节点（两份互相竞争的站点断言）。
			for k, v := range websiteNode(home, title) {
				doc[k] = v
			}
		} else {
			graph = append(graph, websiteNode(home, title))
		}
		graph = append(graph, doc)
		doc = map[string]any{"@context": schemaOrgContext, "@graph": graph}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	return "<script type=\"application/ld+json\">" + string(b) + "</script>"
}

// breadcrumbList 由 URL 路径生成面包屑（/a/b → 首页 + a + b）。
func breadcrumbList(rawURL, homeName string) []map[string]any {
	homeName = strings.TrimSpace(homeName)
	if homeName == "" {
		homeName = "Home"
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Opaque != "" {
		return nil
	}
	// 路径不是基址；查询和锚点也不参与层级。保留编码后的路径段，
	// 否则 a%2Fb 会被错误拆成两级，只有展示名称才进行解码。
	base := ""
	if u.Host != "" {
		base = "//" + u.Host
		if u.Scheme != "" {
			base = u.Scheme + ":" + base
		}
	}
	parts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	out := []map[string]any{{"@type": "ListItem", "position": 1, "name": homeName, "item": base + "/"}}
	cur := base
	for _, p := range parts {
		cur += "/" + p
		if p == "" {
			continue
		}
		name, _ := url.PathUnescape(p) // EscapedPath 已保证转义合法。
		out = append(out, map[string]any{
			"@type": "ListItem", "position": len(out) + 1, "name": name, "item": cur,
		})
	}
	return out
}

func seoDefaultString(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
