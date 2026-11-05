package compliance

import (
	json "go_wp/pkg/jsonx"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"go_wp/pkg/seo"
)

var (
	reHeadStart = regexp.MustCompile(`(?is)<head\b[^>]*>`)
	reHeadEnd   = regexp.MustCompile(`(?is)</head\s*>`)
	reTitleTag  = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title\s*>`)
	reLinkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	reMetaTag   = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	reJSONLDTag = regexp.MustCompile(`(?is)<script\b[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script\s*>`)
	reTagAttr   = regexp.MustCompile(`(?is)([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>]+))`)
)

type headDoc struct {
	Title      string
	Canonicals []string
	Robots     string
	Hreflangs  []hreflangRef
}

type hreflangRef struct {
	Lang string
	Href string
}

func headOf(raw string) string {
	start := reHeadStart.FindStringIndex(raw)
	if start == nil {
		return raw
	}
	rest := raw[start[1]:]
	end := reHeadEnd.FindStringIndex(rest)
	if end == nil {
		return rest
	}
	return rest[:end[0]]
}

func parseHead(head string) headDoc {
	var doc headDoc
	if m := reTitleTag.FindStringSubmatch(head); m != nil {
		doc.Title = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	for _, tag := range reLinkTag.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		rel := attrs["rel"]
		switch {
		case hasToken(rel, "canonical"):
			doc.Canonicals = append(doc.Canonicals, html.UnescapeString(strings.TrimSpace(attrs["href"])))
		case hasToken(rel, "alternate"):
			lang := strings.TrimSpace(attrs["hreflang"])
			if lang == "" {
				continue
			}
			doc.Hreflangs = append(doc.Hreflangs, hreflangRef{
				Lang: html.UnescapeString(lang),
				Href: html.UnescapeString(strings.TrimSpace(attrs["href"])),
			})
		}
	}
	for _, tag := range reMetaTag.FindAllString(head, -1) {
		attrs := tagAttrs(tag)
		if strings.EqualFold(strings.TrimSpace(attrs["name"]), "robots") {
			doc.Robots = html.UnescapeString(strings.TrimSpace(attrs["content"]))
		}
	}
	return doc
}

func jsonLDScripts(doc string) []string {
	matches := reJSONLDTag.FindAllStringSubmatch(doc, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func tagAttrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range reTagAttr.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(m[1])
		val := ""
		switch {
		case m[2] != "":
			val = m[2]
		case m[3] != "":
			val = m[3]
		default:
			val = m[4]
		}

		if _, exists := out[name]; !exists {
			out[name] = html.UnescapeString(val)
		}
	}
	return out
}

func hasToken(value, token string) bool {
	for _, f := range strings.Fields(value) {
		if strings.EqualFold(f, token) {
			return true
		}
	}
	return false
}

func pathOf(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") && !strings.Contains(s, "://") {

		return "/" + strings.TrimPrefix(s, "./")
	}
	u, err := url.Parse(s)
	if err != nil || u.Opaque != "" {
		return s
	}
	if u.Path == "" {
		return "/"
	}
	return u.Path
}

func isExternal(raw, base string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if u.Opaque != "" {
		return true
	}
	if u.Host == "" {
		return u.Scheme != ""
	}
	if u.Scheme == "" {
		return true
	}
	b, err := url.Parse(strings.TrimSpace(base))
	if err != nil || (b.Scheme != "http" && b.Scheme != "https") || b.Host == "" || b.User != nil || b.RawQuery != "" || b.Fragment != "" {
		return true
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil ||
		!strings.EqualFold(u.Scheme, b.Scheme) || !strings.EqualFold(u.Hostname(), b.Hostname()) || u.Port() != b.Port() {
		return true
	}
	prefix := strings.TrimRight(b.Path, "/")
	if prefix == "" {
		return false
	}

	if strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return true
	}
	clean := path.Clean(u.Path)
	return clean != prefix && !strings.HasPrefix(clean, prefix+"/")
}

func canonicalPathOf(raw, base string) string {
	path := pathOf(raw)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || isExternal(raw, base) {
		return path
	}
	b, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return path
	}
	prefix := strings.TrimRight(b.Path, "/")
	if prefix != "" {
		path = strings.TrimPrefix(path, prefix)
		if path == "" {
			return "/"
		}
	}
	return path
}

func ruleForLang(langs []LangRule, code string) (LangRule, bool) {
	c := strings.TrimSpace(code)
	if c == "" {
		return LangRule{}, false
	}
	for _, l := range langs {
		if strings.EqualFold(strings.TrimSpace(l.Code), c) {
			return l, true
		}
	}
	return LangRule{}, false
}

func ruleForPath(langs []LangRule, path string) (LangRule, bool) {
	p := seo.CanonicalPublicPath(path)
	best := LangRule{}
	found := false
	for _, l := range langs {
		prefix := strings.TrimSpace(l.Prefix)
		if prefix == "" {
			continue
		}
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			if !found || len(prefix) > len(strings.TrimSpace(best.Prefix)) {
				best, found = l, true
			}
		}
	}
	if found {
		return best, true
	}
	for _, l := range langs {
		if strings.TrimSpace(l.Prefix) == "" {
			return l, true
		}
	}
	return LangRule{}, false
}

func mainEntities(v any) []map[string]any {
	out := []map[string]any{}
	switch doc := v.(type) {
	case map[string]any:
		if graph, ok := doc["@graph"].([]any); ok {
			for _, item := range graph {
				if node, ok := item.(map[string]any); ok && !isSiteNode(node) {
					out = append(out, node)
				}
			}
			return out
		}
		if !isSiteNode(doc) {
			out = append(out, doc)
		}
	case []any:
		for _, item := range doc {
			if node, ok := item.(map[string]any); ok && !isSiteNode(node) {
				out = append(out, node)
			}
		}
	}
	return out
}

func isSiteNode(node map[string]any) bool {
	for _, t := range typeNames(node) {
		switch t {
		case "organization", "website", "breadcrumblist":
			return true
		}
	}
	return false
}

func typeNames(node map[string]any) []string {
	switch t := node["@type"].(type) {
	case string:
		return []string{strings.ToLower(strings.TrimSpace(t))}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, strings.ToLower(strings.TrimSpace(s)))
			}
		}
		return out
	}
	return nil
}

func nodeString(node map[string]any, key string) string {
	s, _ := node[key].(string)
	return strings.TrimSpace(s)
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func jsonUnmarshalAny(data []byte) (any, error) {
	var v any
	err := json.Unmarshal(data, &v)
	return v, err
}
