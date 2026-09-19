package seo

// sitemap_alternates_group_test.go — sitemap <url> 节点内语言分组（xhtml:link）的自洽约束。
//
// 规则与构建期 head 的 alternateLinks（internal/builder/seo_head.go:158）逐条对齐：
// 至少两种语言才输出、同语言去重、语言码升序、x-default 只取第一条且固定最后。
// 两处产物在同一站点上描述同一批互指，规则分叉即自相矛盾的声明，因此这几条要在
// seo 层（产出 XML 的地方）自己成立，而不是只依赖调用方（publication 装配层）过滤。

import (
	"strings"
	"testing"
)

// altEntry 构造一条带互指的 sitemap 条目。
func altEntry(loc string, alts ...SitemapAlternate) SitemapEntry {
	return SitemapEntry{Loc: loc, ChangeFreq: "weekly", Priority: "0.7", Alternates: alts}
}

// buildOne 构建单条条目的 sitemap。
func buildOne(t *testing.T, e SitemapEntry) string {
	t.Helper()
	out, err := BuildSitemap([]SitemapEntry{e})
	if err != nil {
		t.Fatalf("BuildSitemap: %v", err)
	}
	return out
}

// TestBuildSitemapSingleLangDropsGroup 只有一种语言时（哪怕带 x-default 自指）
// 不输出任何 xhtml:link，且产物与「完全没有互指」逐字节一致（不声明 xhtml 命名空间）。
func TestBuildSitemapSingleLangDropsGroup(t *testing.T) {
	// 单条互指（自指）无意义：不输出。
	one := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/about"}))
	if strings.Contains(one, "xhtml") {
		t.Fatalf("单语言不应输出 xhtml:link\n%s", one)
	}
	// 唯一语言 + x-default：x-default 不是一种语言，仍属单语言。
	withDefault := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/about"},
		SitemapAlternate{Lang: "x-default", Href: "https://e.com/about"}))
	if strings.Contains(withDefault, "xhtml") {
		t.Fatalf("唯一语言 + x-default 仍属单语言，不应输出分组\n%s", withDefault)
	}
	// 与无互指的同一 URL 条目逐字节一致。
	bare := buildOne(t, SitemapEntry{Loc: "https://e.com/about", ChangeFreq: "weekly", Priority: "0.7"})
	if bare != withDefault {
		t.Fatalf("单语言分组字节应退化为无互指形态\n无互指:\n%s\n有互指:\n%s", bare, withDefault)
	}
	if !strings.Contains(withDefault, "<loc>https://e.com/about</loc>") {
		t.Fatalf("url 节点本身必须保留\n%s", withDefault)
	}
}

// TestBuildSitemapMultiLangOrderIndependent 多语言分组：语言码升序、顺序确定且与输入顺序无关。
func TestBuildSitemapMultiLangOrderIndependent(t *testing.T) {
	asc := []SitemapAlternate{
		{Lang: "en-US", Href: "https://e.com/en-US/about"},
		{Lang: "ja-JP", Href: "https://e.com/ja-JP/about"},
		{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
	}
	sorted := buildOne(t, altEntry("https://e.com/about", asc...))
	// 打乱输入顺序：产物必须逐字节相同（「同输入同字节」的更强形态）。
	shuffled := buildOne(t, altEntry("https://e.com/about", asc[2], asc[0], asc[1]))
	if sorted != shuffled {
		t.Fatalf("分组输出依赖输入顺序\n升序输入:\n%s\n乱序输入:\n%s", sorted, shuffled)
	}
	if got := strings.Count(sorted, "<xhtml:link"); got != 3 {
		t.Fatalf("应输出 3 条互指，实际 %d\n%s", got, sorted)
	}
	if !strings.Contains(sorted, "xmlns:xhtml=\"http://www.w3.org/1999/xhtml\"") {
		t.Fatalf("缺少 xhtml 命名空间声明\n%s", sorted)
	}
	iEn := strings.Index(sorted, "hreflang=\"en-US\"")
	iJa := strings.Index(sorted, "hreflang=\"ja-JP\"")
	iZh := strings.Index(sorted, "hreflang=\"zh-CN\"")
	if iEn < 0 || iJa < 0 || iZh < 0 || iEn > iJa || iJa > iZh {
		t.Fatalf("互指未按语言码升序（en-US < ja-JP < zh-CN）\n%s", sorted)
	}
	// 确定性：同输入两次字节一致。
	if again := buildOne(t, altEntry("https://e.com/about", asc...)); again != sorted {
		t.Fatal("同输入两次输出不一致")
	}
}

// TestBuildSitemapXDefaultRule 与 seo_head.go 一致：x-default 只取第一条、
// 恰好一条、固定最后，且 href 指向已列出的语言版本；没有默认标记时不输出。
func TestBuildSitemapXDefaultRule(t *testing.T) {
	multi := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
		SitemapAlternate{Lang: "en-US", Href: "https://e.com/en-US/about"},
		SitemapAlternate{Lang: "x-default", Href: "https://e.com/zh-CN/about"},
		SitemapAlternate{Lang: "x-default", Href: "https://e.com/en-US/about"},
	))
	if got := strings.Count(multi, "hreflang=\"x-default\""); got != 1 {
		t.Fatalf("x-default 应恰好 1 条（只取第一条），实际 %d\n%s", got, multi)
	}
	if !strings.Contains(multi, "hreflang=\"x-default\" href=\"https://e.com/zh-CN/about\"") {
		t.Fatalf("x-default 应取第一条默认版本\n%s", multi)
	}
	if strings.Index(multi, "hreflang=\"zh-CN\"") > strings.Index(multi, "hreflang=\"x-default\"") {
		t.Fatalf("x-default 应固定最后输出\n%s", multi)
	}
	// x-default 的 href 必须等于某个已列出的语言版本（不引入新 URL：zh-CN 出现两次）。
	if got := strings.Count(multi, "https://e.com/zh-CN/about"); got != 2 {
		t.Fatalf("x-default 应复用已列出的语言 URL，zh-CN 应出现 2 次，实际 %d\n%s", got, multi)
	}
	// 没有默认标记：一条 x-default 都不输出。
	noDefault := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
		SitemapAlternate{Lang: "en-US", Href: "https://e.com/en-US/about"},
	))
	if strings.Contains(noDefault, "x-default") {
		t.Fatalf("无默认标记时不应输出 x-default\n%s", noDefault)
	}
}

// TestBuildSitemapDuplicateLangDeduped 同一 hreflang 只输出一条（重复标注属无效声明）。
func TestBuildSitemapDuplicateLangDeduped(t *testing.T) {
	out := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "en-US", Href: "https://e.com/en-US/about"},
		SitemapAlternate{Lang: "en-US", Href: "https://e.com/en/about"},
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
	))
	if got := strings.Count(out, "hreflang=\"en-US\""); got != 1 {
		t.Fatalf("en-US 应只输出 1 条，实际 %d\n%s", got, out)
	}
	if !strings.Contains(out, "hreflang=\"en-US\" href=\"https://e.com/en-US/about\"") {
		t.Fatalf("同语言应保留第一条\n%s", out)
	}
	if got := strings.Count(out, "<xhtml:link"); got != 2 {
		t.Fatalf("应输出 2 条互指（en-US/zh-CN），实际 %d\n%s", got, out)
	}
}
