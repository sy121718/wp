package builder

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
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

// schemaTypeMap 结构化数据类型映射（空 = WebPage）。
var schemaTypeMap = map[string]string{
	"website": "WebSite", "article": "Article", "product": "Product", "faq": "FAQPage",
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
		"@context": "https://schema.org",
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
