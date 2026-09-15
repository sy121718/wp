package seo

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// sitemapNamespace sitemap 协议命名空间（urlset 与 sitemapindex 共用）。
const sitemapNamespace = "http://www.sitemaps.org/schemas/sitemap/0.9"

// SitemapFileName 站点 sitemap 的主文件名：小站是 sitemap 本体，大站是分片索引。
const SitemapFileName = "sitemap.xml"

// SitemapShardLimit 单个 sitemap 文件的 URL 上限（审计 SEO-011）。
//
// sitemap 协议上限是 5 万条 URL / 50MB，这里取 1 万留余量：多语言站点单条 URL
// 还要带 xhtml:link 互指，条目文本比单语言长几倍 —— 贴着 5 万写会先撞 50MB
// 而不是 5 万条，而超限的后果是整份 sitemap 被搜索引擎丢弃。
const SitemapShardLimit = 10000

// SitemapShardName 分片文件名（i 从 1 开始）：sitemap-1.xml、sitemap-2.xml……
func SitemapShardName(i int) string {
	return fmt.Sprintf("sitemap-%d.xml", i)
}

// sitemapIndexDoc / sitemapIndexNode sitemap 索引的 XML 结构。
type sitemapIndexDoc struct {
	XMLName  xml.Name           `xml:"sitemapindex"`
	Xmlns    string             `xml:"xmlns,attr"`
	Sitemaps []sitemapIndexNode `xml:"sitemap"`
}

type sitemapIndexNode struct {
	Loc string `xml:"loc"`
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
	sorted := normalizeEntries(entries)
	set := urlSet{Xmlns: sitemapNamespace}
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
//
// 分片（审计 SEO-011）：条目数 ≤ SitemapShardLimit 时行为与分片之前逐字节一致
// （单个 sitemap.xml，robots.txt 指过去）；超过上限时 sitemap.xml 变成 sitemap 索引，
// 各片写进 sitemap-1.xml、sitemap-2.xml……。索引与单文件占同一个路径，
// 因此 robots.txt 与调用方（publication）都不必知道站点是大站还是小站。
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
	// 清掉上一次留下的分片：URL 数从 2 万降到 100 后，sitemap-2.xml 里还留着已下线的
	// 地址，索引却不再引用它 —— 爬虫按旧索引继续抓的后果比"文件不存在"更糟。
	if err := pruneSitemapShards(dir, written); err != nil {
		return err
	}
	rb := BuildRobots(baseURL, "/"+SitemapFileName)
	return writeFileAtomic(filepath.Join(dir, "robots.txt"), []byte(rb))
}

// normalizeEntries sitemap 条目的规范形态：去重（同 Loc 只留第一条）+ 按 Loc 升序。
//
// 排序用 sort.SliceStable 而不是插入排序：SEO-011 的分片场景本就是大站，
// 上万条 URL 走 O(n²) 会让每次发布多花几十秒。去重后 Loc 唯一，
// 稳定排序与插入排序的产物逐字节相同（单文件 sitemap 的字节不变量不受影响）。
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].Loc < out[j].Loc })
	return out
}

// ShardEntries 把条目切成若干片（每片 ≤ SitemapShardLimit 条）。
//
// 先按单文件输出的同一口径（去重 + 按 Loc 升序）归一，因此**分片结果与输入顺序无关**：
// 「同一批激活路由产出相同字节」这条 sitemap 不变量在分片站点上依然成立。
// 返回 nil 表示没有任何有效条目（与空输入产出空 urlset 的单文件路径区分开，
// 调用方据 len 判断是否分片）。
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

// BuildSitemapIndex 生成 sitemap 索引（locs 为各分片的绝对 URL，按传入顺序输出）。
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

// pruneSitemapShards 删除本次没写出的分片文件（只认 sitemap-<数字>.xml 这一种名字）。
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

// isSitemapShardName 是否为分片文件名（sitemap-<正整数>.xml）。
func isSitemapShardName(name string) bool {
	num := strings.TrimSuffix(strings.TrimPrefix(name, "sitemap-"), ".xml")
	if num == name || num == "" {
		return false
	}
	n, err := strconv.Atoi(num)
	return err == nil && n > 0
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
