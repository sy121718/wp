package seo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

// sitemap.go — 站点级 SEO 产物（sitemap.xml / robots.txt）。
//
// 发布激活后由调用方刷新：输入是「已激活 URL 列表」，输出是 ActiveRoot 下的两个文件。
// 确定性：条目按 Loc 升序排序后输出，同一输入产生相同字节。

// SitemapAlternate 语言互指条目（sitemap 的 xhtml:link，多语言 P3）。
// Lang 为 BCP 47 语言码，或 "x-default"（默认语言版本）。
type SitemapAlternate struct {
	Lang string
	Href string
}

// SitemapEntry sitemap 单条记录。
type SitemapEntry struct {
	Loc        string // 绝对 URL
	LastMod    string // YYYY-MM-DD（空则省略）
	ChangeFreq string // always/hourly/daily/weekly/monthly/yearly/never（空则省略）
	Priority   string // 0.0-1.0（空则省略）
	// Alternates 该 URL 的其他语言版本（多语言站点按语言分组输出）。
	Alternates []SitemapAlternate
}

// urlSet / urlNode sitemap XML 结构。
type urlSet struct {
	XMLName xml.Name `xml:"urlset"`
	Xmlns   string   `xml:"xmlns,attr"`
	// XmlnsXhtml 仅在存在语言互指时声明（保持单语言 sitemap 字节不变）。
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

// linkNode sitemap 的语言互指标签（rel=alternate）。
type linkNode struct {
	Rel      string `xml:"rel,attr"`
	Hreflang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

// xhtmlNamespace sitemap 语言互指所需命名空间。
const xhtmlNamespace = "http://www.w3.org/1999/xhtml"

// JoinURL 拼接站点基础 URL 与路径（path 以 / 开头）。
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

// BuildSitemap 生成 sitemap.xml 内容（条目按 Loc 升序，确定性输出）。
func BuildSitemap(entries []SitemapEntry) (string, error) {
	sorted := make([]SitemapEntry, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		if strings.TrimSpace(e.Loc) == "" || seen[e.Loc] {
			continue
		}
		seen[e.Loc] = true
		sorted = append(sorted, e)
	}
	// 简单插入排序：URL 数量级（站点页面数）下足够，且避免引入 sort 依赖差异。
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Loc < sorted[j-1].Loc; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	set := urlSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, e := range sorted {
		node := urlNode{
			Loc: e.Loc, LastMod: e.LastMod, ChangeFreq: e.ChangeFreq, Priority: e.Priority,
		}
		// 语言互指：按语言码升序输出、x-default 固定最后（确定性输出）。
		alts := sortAlternates(e.Alternates)
		for _, a := range alts {
			if strings.TrimSpace(a.Href) == "" || strings.TrimSpace(a.Lang) == "" {
				continue
			}
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

// BuildRobots 生成 robots.txt（含 sitemap 地址）。
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

// WriteSiteFiles 把 sitemap.xml 与 robots.txt 写入 dir（覆盖写，原子性由调用方保证）。
func WriteSiteFiles(dir, baseURL string, entries []SitemapEntry) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sm, err := BuildSitemap(entries)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, "sitemap.xml"), []byte(sm)); err != nil {
		return err
	}
	rb := BuildRobots(baseURL, "/sitemap.xml")
	return writeFileAtomic(filepath.Join(dir, "robots.txt"), []byte(rb))
}

// writeFileAtomic 先写同目录临时文件再 rename 落位。
//
// 直接 os.WriteFile 目标文件时，进程若在写入中途崩溃会留下截断的 sitemap.xml，
// 爬虫读到半个 XML 会直接判定解析失败（比文件不存在更糟）。同目录 rename 在同一
// 文件系统上是原子的：读者要么看到旧内容，要么看到完整新内容。
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EntryForPath 由单条「已激活路径」构造 sitemap 条目（根路径优先级最高）。
func EntryForPath(baseURL, path string) SitemapEntry {
	freq, prio := "weekly", "0.7"
	if path == "/" || path == "" {
		freq, prio = "daily", "1.0"
	}
	return SitemapEntry{Loc: JoinURL(baseURL, path), ChangeFreq: freq, Priority: prio}
}

// EntriesFromPaths 由「已激活路径」构造 sitemap 条目（根路径优先级最高）。
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

// sortAlternates 语言互指排序：语言码升序，x-default 固定最后（确定性输出）。
func sortAlternates(in []SitemapAlternate) []SitemapAlternate {
	out := make([]SitemapAlternate, 0, len(in))
	for _, a := range in {
		if a.Lang == "x-default" {
			continue
		}
		out = append(out, a)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Lang < out[j-1].Lang; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	for _, a := range in {
		if a.Lang == "x-default" {
			out = append(out, a)
		}
	}
	return out
}
