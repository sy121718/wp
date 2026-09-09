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

// SitemapEntry sitemap 单条记录。
type SitemapEntry struct {
	Loc        string // 绝对 URL
	LastMod    string // YYYY-MM-DD（空则省略）
	ChangeFreq string // always/hourly/daily/weekly/monthly/yearly/never（空则省略）
	Priority   string // 0.0-1.0（空则省略）
}

// urlSet / urlNode sitemap XML 结构。
type urlSet struct {
	XMLName xml.Name  `xml:"urlset"`
	Xmlns   string    `xml:"xmlns,attr"`
	URLs    []urlNode `xml:"url"`
}

type urlNode struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod,omitempty"`
	ChangeFreq string `xml:"changefreq,omitempty"`
	Priority   string `xml:"priority,omitempty"`
}

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
		set.URLs = append(set.URLs, urlNode{
			Loc: e.Loc, LastMod: e.LastMod, ChangeFreq: e.ChangeFreq, Priority: e.Priority,
		})
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
	if err := os.WriteFile(filepath.Join(dir, "sitemap.xml"), []byte(sm), 0o644); err != nil {
		return err
	}
	rb := BuildRobots(baseURL, "/sitemap.xml")
	return os.WriteFile(filepath.Join(dir, "robots.txt"), []byte(rb), 0o644)
}

// EntriesFromPaths 由「已激活路径」构造 sitemap 条目（根路径优先级最高）。
func EntriesFromPaths(baseURL string, paths []string) []SitemapEntry {
	out := make([]SitemapEntry, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		freq, prio := "weekly", "0.7"
		if p == "/" || p == "" {
			freq, prio = "daily", "1.0"
		}
		out = append(out, SitemapEntry{Loc: JoinURL(baseURL, p), ChangeFreq: freq, Priority: prio})
	}
	return out
}
