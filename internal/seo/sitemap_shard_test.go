package seo

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sitemap_shard_test.go — sitemap 分片与索引（审计 SEO-011）。
//
// 三条不变量：
//  1. 小站（≤ SitemapShardLimit）产物与分片之前逐字节一致，且不产生分片文件；
//  2. 大站 sitemap.xml 是 sitemapindex，各片是合法 urlset 且片内条数不超上限；
//  3. 分片结果与输入顺序无关（确定性），条目缩水后旧分片被清理。

// testURLSet / testSitemapIndex 测试侧解析结构。
//
// 刻意不复用生成侧结构体：这里按 XML 反序列化，生成侧改字段名/漏写标签时
// 测试会真的失败，而不是跟着一起改完还绿。
type testURLSet struct {
	XMLName xml.Name `xml:"urlset"`
	URLs    []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

type testSitemapIndex struct {
	XMLName  xml.Name `xml:"sitemapindex"`
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
}

// shardPaths 生成 n 条互不相同的访问路径。
func shardPaths(n int) []string {
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		paths = append(paths, "/p/"+strconv.Itoa(i))
	}
	return paths
}

// readSiteFile 读取站点级产物文件。
func readSiteFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}

// listShardFiles 目录下现存的分片文件名（已排序）。
func listShardFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "sitemap-*.xml"))
	if err != nil {
		t.Fatalf("glob 分片失败: %v", err)
	}
	out := make([]string, 0, len(names))
	for _, p := range names {
		out = append(out, filepath.Base(p))
	}
	return out
}

// TestWriteSiteFilesSmallSiteUnchanged 现有站点行为不变：单文件 sitemap.xml、零分片、
// 字节与 BuildSitemap 完全一致（分片能力不得改变小站产物）。
func TestWriteSiteFilesSmallSiteUnchanged(t *testing.T) {
	dir := t.TempDir()
	entries := EntriesFromPaths("https://e.com", []string{"/", "/a", "/b"})
	if err := WriteSiteFiles(dir, "https://e.com", entries); err != nil {
		t.Fatalf("WriteSiteFiles: %v", err)
	}
	want, err := BuildSitemap(entries)
	if err != nil {
		t.Fatalf("BuildSitemap: %v", err)
	}
	if got := readSiteFile(t, filepath.Join(dir, SitemapFileName)); got != want {
		t.Errorf("小站 sitemap 与单文件产物不一致:\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, got)
	}
	if shards := listShardFiles(t, dir); len(shards) != 0 {
		t.Errorf("小站不应产生分片，实际 %v", shards)
	}
	robots := readSiteFile(t, filepath.Join(dir, "robots.txt"))
	if !strings.Contains(robots, "Sitemap: https://e.com/sitemap.xml") {
		t.Errorf("robots.txt 应指向 /sitemap.xml:\n%s", robots)
	}
}

// TestWriteSiteFilesShardsLargeSite 超过阈值：生成索引 + 分片，每片可解析且并集完整。
func TestWriteSiteFilesShardsLargeSite(t *testing.T) {
	const n = 2 * SitemapShardLimit
	dir := t.TempDir()
	entries := EntriesFromPaths("https://e.com", shardPaths(n))
	if err := WriteSiteFiles(dir, "https://e.com", entries); err != nil {
		t.Fatalf("WriteSiteFiles: %v", err)
	}

	var index testSitemapIndex
	if err := xml.Unmarshal([]byte(readSiteFile(t, filepath.Join(dir, SitemapFileName))), &index); err != nil {
		t.Fatalf("sitemap.xml 不是可解析的 sitemapindex: %v", err)
	}
	if len(index.Sitemaps) != 2 {
		t.Fatalf("20000 条应分 2 片，索引里 %d 条", len(index.Sitemaps))
	}

	seen := map[string]bool{}
	for i, node := range index.Sitemaps {
		name := SitemapShardName(i + 1)
		if want := "https://e.com/" + name; node.Loc != want {
			t.Errorf("索引第 %d 条 loc=%q，期望 %q", i+1, node.Loc, want)
		}
		var set testURLSet
		if err := xml.Unmarshal([]byte(readSiteFile(t, filepath.Join(dir, name))), &set); err != nil {
			t.Fatalf("%s 不是可解析的 urlset: %v", name, err)
		}
		if len(set.URLs) == 0 || len(set.URLs) > SitemapShardLimit {
			t.Errorf("%s 条数 %d 越界（1..%d）", name, len(set.URLs), SitemapShardLimit)
		}
		prev := ""
		for _, u := range set.URLs {
			if u.Loc <= prev {
				t.Errorf("%s 未按 loc 升序: %q 出现在 %q 之后", name, u.Loc, prev)
			}
			prev = u.Loc
			if seen[u.Loc] {
				t.Errorf("URL 在多个分片重复出现: %s", u.Loc)
			}
			seen[u.Loc] = true
		}
	}
	if len(seen) != n {
		t.Errorf("分片并集覆盖 %d 条，期望 %d", len(seen), n)
	}
	if shards := listShardFiles(t, dir); len(shards) != 2 {
		t.Errorf("应写 2 个分片文件，实际 %v", shards)
	}

	robots := readSiteFile(t, filepath.Join(dir, "robots.txt"))
	if !strings.Contains(robots, "Sitemap: https://e.com/sitemap.xml") {
		t.Errorf("大站 robots.txt 仍应指向索引 /sitemap.xml:\n%s", robots)
	}
}

// TestShardEntriesBoundary 阈值边界：正好上限仍是单文件，多一条才分片。
func TestShardEntriesBoundary(t *testing.T) {
	base := "https://e.com"
	atLimit := EntriesFromPaths(base, shardPaths(SitemapShardLimit))
	if got := len(ShardEntries(atLimit)); got != 1 {
		t.Errorf("%d 条应仍是一片，实际 %d", SitemapShardLimit, got)
	}
	over := EntriesFromPaths(base, shardPaths(SitemapShardLimit+1))
	shards := ShardEntries(over)
	if len(shards) != 2 {
		t.Fatalf("%d 条应分两片，实际 %d", SitemapShardLimit+1, len(shards))
	}
	if len(shards[0]) != SitemapShardLimit || len(shards[1]) != 1 {
		t.Errorf("分片切分错误: %d + %d", len(shards[0]), len(shards[1]))
	}
	// 空输入不产生分片（调用方据此区分「无条目」与「一片」）。
	if got := ShardEntries(nil); got != nil {
		t.Errorf("空输入应返回 nil，实际 %v", got)
	}

	dir := t.TempDir()
	if err := WriteSiteFiles(dir, base, over); err != nil {
		t.Fatalf("WriteSiteFiles: %v", err)
	}
	var second testURLSet
	if err := xml.Unmarshal([]byte(readSiteFile(t, filepath.Join(dir, SitemapShardName(2)))), &second); err != nil {
		t.Fatalf("第二片解析失败: %v", err)
	}
	if len(second.URLs) != 1 {
		t.Errorf("第二片应 1 条，实际 %d", len(second.URLs))
	}
}

// TestShardEntriesOrderIndependent 同一集合不同输入顺序 → 分片与索引逐字节一致。
func TestShardEntriesOrderIndependent(t *testing.T) {
	const n = SitemapShardLimit + 500
	base := "https://e.com"
	paths := shardPaths(n)
	reversed := make([]string, 0, n)
	for i := len(paths) - 1; i >= 0; i-- {
		reversed = append(reversed, paths[i])
	}
	shardOf := func(in []string) []string {
		out := make([]string, 0, 2)
		for _, shard := range ShardEntries(EntriesFromPaths(base, in)) {
			content, err := BuildSitemap(shard)
			if err != nil {
				t.Fatalf("BuildSitemap: %v", err)
			}
			out = append(out, content)
		}
		return out
	}
	a, b := shardOf(paths), shardOf(reversed)
	if len(a) != len(b) {
		t.Fatalf("分片数不一致: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("第 %d 片与输入顺序相关（确定性被破坏）", i+1)
		}
	}
}

// TestWriteSiteFilesPrunesStaleShards 条目缩水到单文件后，上一次留下的分片必须清掉。
func TestWriteSiteFilesPrunesStaleShards(t *testing.T) {
	base := "https://e.com"
	dir := t.TempDir()
	if err := WriteSiteFiles(dir, base, EntriesFromPaths(base, shardPaths(2*SitemapShardLimit))); err != nil {
		t.Fatalf("首次写入: %v", err)
	}
	if got := len(listShardFiles(t, dir)); got != 2 {
		t.Fatalf("首次应 2 个分片，实际 %d", got)
	}
	if err := WriteSiteFiles(dir, base, EntriesFromPaths(base, []string{"/", "/only"})); err != nil {
		t.Fatalf("二次写入: %v", err)
	}
	if shards := listShardFiles(t, dir); len(shards) != 0 {
		t.Errorf("旧分片应被清理，仍存在 %v", shards)
	}
	var set testURLSet
	if err := xml.Unmarshal([]byte(readSiteFile(t, filepath.Join(dir, SitemapFileName))), &set); err != nil {
		t.Fatalf("缩片后 sitemap.xml 应为 urlset: %v", err)
	}
	if len(set.URLs) != 2 {
		t.Errorf("缩片后应 2 条，实际 %d", len(set.URLs))
	}
}

// TestIsSitemapShardName 分片名判定：只认 sitemap-<正整数>.xml（清理时的安全边界）。
func TestIsSitemapShardName(t *testing.T) {
	for name, want := range map[string]bool{
		"sitemap-1.xml":     true,
		"sitemap-42.xml":    true,
		"sitemap.xml":       false,
		"sitemap-.xml":      false,
		"sitemap-x.xml":     false,
		"sitemap-0.xml":     false,
		"feed.xml":          false,
		"sitemap-1.xml.bak": false,
	} {
		if got := isSitemapShardName(name); got != want {
			t.Errorf("isSitemapShardName(%q) = %v，期望 %v", name, got, want)
		}
	}
}
