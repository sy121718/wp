package seo

import (
	"cmp"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type SitemapAlternate struct {
	Lang string
	Href string
}

type SitemapEntry struct {
	Loc        string
	LastMod    string
	ChangeFreq string
	Priority   string

	Alternates []SitemapAlternate
}

type urlSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`

	XmlnsXhtml string    `xml:"xmlns:xhtml,attr,omitempty"`
	URLs       []urlNode `xml:"url"`
}

type urlNode struct {
	Loc        string     `xml:"loc"`
	LastMod    string     `xml:"lastmod,omitempty"`
	ChangeFreq string     `xml:"changefreq,omitempty"`
	Priority   string     `xml:"priority,omitempty"`
	Links      []linkNode `xml:"xhtml:link,omitempty"`
}

type linkNode struct {
	Rel      string `xml:"rel,attr"`
	Hreflang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

const xhtmlNamespace = "http://www.w3.org/1999/xhtml"

const sitemapNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

const SitemapFileName = "sitemap.xml"

const SitemapShardLimit = 10000

func SitemapShardName(i int) string {
	return fmt.Sprintf("sitemap-%d.xml", i)
}

type sitemapIndexDoc struct {
	XMLName  xml.Name           `xml:"sitemapindex"`
	Xmlns    string             `xml:"xmlns,attr"`
	Sitemaps []sitemapIndexNode `xml:"sitemap"`
}

type sitemapIndexNode struct {
	Loc string `xml:"loc"`
}

func JoinURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func BuildSitemap(entries []SitemapEntry) (string, error) {
	sorted := normalizeEntries(entries)
	set := urlSet{Xmlns: sitemapNamespace}
	for _, e := range sorted {
		node := urlNode{
			Loc: e.Loc, LastMod: e.LastMod, ChangeFreq: e.ChangeFreq, Priority: e.Priority,
		}

		for _, a := range normalizeAlternates(e.Alternates) {
			node.Links = append(node.Links, linkNode{Rel: "alternate", Hreflang: a.Lang, Href: a.Href})
		}
		if len(node.Links) > 0 {
			set.XmlnsXhtml = xhtmlNamespace
		}
		set.URLs = append(set.URLs, node)
	}
	body, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(body) + "\n", nil
}

func BuildRobots(baseURL, sitemapPath string) string {
	if sitemapPath == "" {
		sitemapPath = "/sitemap.xml"
	}
	var sb strings.Builder
	sb.WriteString("User-agent: *\n")
	sb.WriteString("Allow: /\n")
	sb.WriteString("Disallow: /admin\n")
	sb.WriteString("Disallow: /api/\n")
	sb.WriteString("Disallow: /storage/\n")
	if baseURL != "" {
		sb.WriteString("\nSitemap: ")
		sb.WriteString(JoinURL(baseURL, sitemapPath))
		sb.WriteString("\n")
	}
	return sb.String()
}

func WriteSiteFiles(dir, baseURL string, entries []SitemapEntry) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	shards := ShardEntries(entries)
	written := map[string]bool{}
	if len(shards) > 1 {
		locs := make([]string, 0, len(shards))
		for i, shard := range shards {
			content, err := BuildSitemap(shard)
			if err != nil {
				return err
			}
			name := SitemapShardName(i + 1)
			if err := writeFileAtomic(filepath.Join(dir, name), []byte(content)); err != nil {
				return err
			}
			written[name] = true
			locs = append(locs, JoinURL(baseURL, "/"+name))
		}
		index, err := BuildSitemapIndex(locs)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(dir, SitemapFileName), []byte(index)); err != nil {
			return err
		}
	} else {
		sm, err := BuildSitemap(entries)
		if err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(dir, SitemapFileName), []byte(sm)); err != nil {
			return err
		}
	}

	if err := pruneSitemapShards(dir, written); err != nil {
		return err
	}
	rb := BuildRobots(baseURL, "/"+SitemapFileName)
	return writeFileAtomic(filepath.Join(dir, "robots.txt"), []byte(rb))
}

func normalizeEntries(entries []SitemapEntry) []SitemapEntry {
	out := make([]SitemapEntry, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		if strings.TrimSpace(e.Loc) == "" || seen[e.Loc] {
			continue
		}
		seen[e.Loc] = true
		out = append(out, e)
	}
	slices.SortStableFunc(out, func(a, b SitemapEntry) int { return cmp.Compare(a.Loc, b.Loc) })
	return out
}

func ShardEntries(entries []SitemapEntry) [][]SitemapEntry {
	normalized := normalizeEntries(entries)
	if len(normalized) == 0 {
		return nil
	}
	shards := make([][]SitemapEntry, 0, (len(normalized)+SitemapShardLimit-1)/SitemapShardLimit)
	for start := 0; start < len(normalized); start += SitemapShardLimit {
		end := start + SitemapShardLimit
		if end > len(normalized) {
			end = len(normalized)
		}
		shards = append(shards, normalized[start:end])
	}
	return shards
}

func BuildSitemapIndex(locs []string) (string, error) {
	doc := sitemapIndexDoc{Xmlns: sitemapNamespace}
	for _, l := range locs {
		if strings.TrimSpace(l) == "" {
			continue
		}
		doc.Sitemaps = append(doc.Sitemaps, sitemapIndexNode{Loc: l})
	}
	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return xml.Header + string(body) + "\n", nil
}

func pruneSitemapShards(dir string, keep map[string]bool) error {
	names, err := filepath.Glob(filepath.Join(dir, "sitemap-*.xml"))
	if err != nil {
		return err
	}
	for _, path := range names {
		name := filepath.Base(path)
		if keep[name] || !isSitemapShardName(name) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func isSitemapShardName(name string) bool {
	num := strings.TrimSuffix(strings.TrimPrefix(name, "sitemap-"), ".xml")
	if num == name || num == "" {
		return false
	}
	n, err := strconv.Atoi(num)
	return err == nil && n > 0
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func EntryForPath(baseURL, path string) SitemapEntry {
	freq, prio := "weekly", "0.7"
	if path == "/" || path == "" {
		freq, prio = "daily", "1.0"
	}
	return SitemapEntry{Loc: JoinURL(baseURL, path), ChangeFreq: freq, Priority: prio}
}

func EntriesFromPaths(baseURL string, paths []string) []SitemapEntry {
	out := make([]SitemapEntry, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, EntryForPath(baseURL, p))
	}
	return out
}

func normalizeAlternates(in []SitemapAlternate) []SitemapAlternate {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	main := make([]SitemapAlternate, 0, len(in))
	xdefault := ""
	for _, a := range in {
		lang := strings.TrimSpace(a.Lang)
		href := strings.TrimSpace(a.Href)
		if lang == "" || href == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		if lang == "x-default" {
			if xdefault == "" {
				xdefault = href
			}
			continue
		}
		main = append(main, SitemapAlternate{Lang: lang, Href: href})
	}
	if len(main) < 2 {
		return nil
	}
	slices.SortStableFunc(main, func(a, b SitemapAlternate) int { return cmp.Compare(a.Lang, b.Lang) })
	if xdefault == "" {
		return main
	}
	return append(main, SitemapAlternate{Lang: "x-default", Href: xdefault})
}
