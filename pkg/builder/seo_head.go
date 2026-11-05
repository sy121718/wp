package builder

import (
	"fmt"
	"go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
	"html"
	"net/url"
	"os"
	"strings"

	"go_wp/pkg/seo"
)

type Alternate struct {
	Lang    string
	Href    string
	Default bool
}

const (
	robotIndex    = "index"
	robotNoIndex  = "noindex"
	robotFollow   = "follow"
	robotNoFollow = "nofollow"
)

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

func ogType(schemaType string) string {
	switch strings.ToLower(strings.TrimSpace(schemaType)) {
	case "article":
		return "article"
	case "product":
		return "product"
	}
	return "website"
}

func BuildSEOHead(seo SEO, pageURL, title, description, breadcrumbHome string, alternates []Alternate) string {
	if title == "" {
		title = seo.Title
	}
	if description == "" {
		description = seo.Description
	}
	var sb strings.Builder
	esc := html.EscapeString

	canonical := absoluteURL(canonicalPublicPath(strings.TrimSpace(seo.Canonical)))
	if canonical == "" {
		canonical = absoluteURL(canonicalPublicPath(strings.TrimSpace(pageURL)))
	}
	if canonical != "" {
		fmt.Fprintf(&sb, "<link rel=\"canonical\" href=\"%s\">\n", esc(canonical))
	}
	pubURL := absoluteURL(canonicalPublicPath(strings.TrimSpace(pageURL)))
	if pubURL == "" {
		pubURL = canonical
	}
	ogImage := absoluteURL(strings.TrimSpace(seo.OGImage))
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
	if ogImage != "" {
		fmt.Fprintf(&sb, "<meta property=\"og:image\" content=\"%s\">\n", esc(ogImage))
		fmt.Fprintf(&sb, "<meta name=\"twitter:image\" content=\"%s\">\n", esc(ogImage))
		fmt.Fprint(&sb, "<meta name=\"twitter:card\" content=\"summary_large_image\">\n")
	} else {
		fmt.Fprint(&sb, "<meta name=\"twitter:card\" content=\"summary\">\n")
	}
	if ld := buildJSONLD(canonical, title, description, ogImage, seo.SchemaType, seo.ProductOffer, breadcrumbHome); ld != "" {
		sb.WriteString(ld)
		sb.WriteString("\n")
	}
	sb.WriteString(alternateLinks(normalizeAlternates(alternates)))
	return strings.TrimRight(sb.String(), "\n")
}

func siteBasePath() string {
	base := strings.TrimSpace(os.Getenv(siteBaseURLEnv))
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return strings.TrimRight(u.Path, "/")
}

func absoluteURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(u, "//") {
		return u
	}
	base := strings.TrimSpace(os.Getenv(siteBaseURLEnv))
	if base == "" {
		return u
	}
	return seo.JoinURL(base, u)
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

const schemaOrgContext = "https://schema.org"

const siteBaseURLEnv = "WP_SITE_BASE_URL"

const siteSearchQueryParam = "q"

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
	if base := strings.TrimSpace(os.Getenv(siteBaseURLEnv)); base != "" {
		if bu, berr := url.Parse(base); berr == nil {
			prefix := strings.TrimRight(bu.Path, "/")
			if prefix != "" {
				p := strings.TrimRight(u.Path, "/")
				if strings.HasPrefix(p, prefix) {
					u.Path = strings.TrimPrefix(p, prefix)
				}
			}
		}
	}
	return u.Path == "" || u.Path == "/"
}

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

var schemaTypeMap = map[string]string{
	"website": "WebSite", "article": "Article", "product": "Product",
}

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
				"@type": "Offer",
				"price": offer.Price,

				"priceCurrency": seoDefaultString(offer.PriceCurrency, i18n.GetDefaultCurrency()),
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

	if home := homeSiteRoot(url, title); home != "" {
		graph := make([]any, 0, 3)
		graph = append(graph, organizationNode(home, title, image))
		if typeName == "WebSite" {

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

	base := ""
	if u.Host != "" {
		base = "//" + u.Host
		if u.Scheme != "" {
			base = u.Scheme + ":" + base
		}
	}

	escapedPath := u.EscapedPath()
	if base := strings.TrimSpace(os.Getenv(siteBaseURLEnv)); base != "" {
		if bu, berr := url.Parse(base); berr == nil {
			prefix := strings.TrimRight(bu.EscapedPath(), "/")
			if prefix != "" && strings.HasPrefix(escapedPath, prefix) {
				escapedPath = strings.TrimPrefix(escapedPath, prefix)
			}
		}
	}
	parts := strings.Split(strings.TrimPrefix(escapedPath, "/"), "/")

	out := []map[string]any{{"@type": "ListItem", "position": 1, "name": homeName, "item": base + siteBasePath() + "/"}}

	cur := base + siteBasePath()
	for _, p := range parts {
		cur += "/" + p
		if p == "" {
			continue
		}
		name, _ := url.PathUnescape(p)
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
