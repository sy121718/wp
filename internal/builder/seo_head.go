package builder

import (
	"encoding/json"
	"fmt"
	"html"
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

// BuildSEOHead 生成注入 <head> 的 SEO 片段（canonical / OG / Twitter / JSON-LD / hreflang）。
// pageURL 为页面最终 URL（空则省略 URL 相关标签）。
// alternates 为同页其他语言版本（空 = 单语言站点，输出与 P3 之前逐字节一致）；
// 至少两种语言时才输出 hreflang（自指单条无意义）。
func BuildSEOHead(seo SEO, pageURL, title, description string, alternates []Alternate) string {
	if title == "" {
		title = seo.Title
	}
	if description == "" {
		description = seo.Description
	}
	var sb strings.Builder
	esc := html.EscapeString

	canonical := strings.TrimSpace(seo.Canonical)
	if canonical == "" {
		canonical = strings.TrimSpace(pageURL)
	}
	if canonical != "" {
		fmt.Fprintf(&sb, "<link rel=\"canonical\" href=\"%s\">\n", esc(canonical))
	}
	if title != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:title\" content=\"%s\">\n", esc(title))
		fmt.Fprintf(&sb, "<meta name=\"twitter:title\" content=\"%s\">\n", esc(title))
	}
	if description != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:description\" content=\"%s\">\n", esc(description))
		fmt.Fprintf(&sb, "<meta name=\"twitter:description\" content=\"%s\">\n", esc(description))
	}
	if pageURL != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:url\" content=\"%s\">\n", esc(pageURL))
	}
	fmt.Fprint(&sb, "<meta property=\"og:type\" content=\"website\">\n")
	if seo.OGImage != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:image\" content=\"%s\">\n", esc(seo.OGImage))
		fmt.Fprintf(&sb, "<meta name=\"twitter:image\" content=\"%s\">\n", esc(seo.OGImage))
		fmt.Fprint(&sb, "<meta name=\"twitter:card\" content=\"summary_large_image\">\n")
	} else {
		fmt.Fprint(&sb, "<meta name=\"twitter:card\" content=\"summary\">\n")
	}
	if ld := buildJSONLD(canonical, title, description, seo.OGImage, seo.SchemaType); ld != "" {
		sb.WriteString(ld)
		sb.WriteString("\n")
	}
	sb.WriteString(alternateLinks(alternates))
	return strings.TrimRight(sb.String(), "\n")
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
func buildJSONLD(url, title, description, image, schemaType string) string {
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
	if crumbs := breadcrumbList(url); len(crumbs) > 1 {
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
func breadcrumbList(url string) []map[string]any {
	u := strings.TrimSpace(url)
	if u == "" {
		return nil
	}
	// 去掉协议与主机，仅取路径段。
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
		if j := strings.Index(u, "/"); j >= 0 {
			u = u[j:]
		} else {
			u = "/"
		}
	}
	base := strings.TrimRight(url, "/")
	if i := strings.Index(base, "://"); i >= 0 {
		rest := base[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			base = base[:i+3] + rest[:j]
		}
	}
	parts := strings.Split(strings.Trim(u, "/"), "/")
	out := []map[string]any{{"@type": "ListItem", "position": 1, "name": "Home", "item": base + "/"}}
	cur := base
	for i, p := range parts {
		if p == "" {
			continue
		}
		cur += "/" + p
		out = append(out, map[string]any{
			"@type": "ListItem", "position": i + 2, "name": p, "item": cur,
		})
	}
	return out
}
