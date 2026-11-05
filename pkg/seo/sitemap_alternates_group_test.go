package seo

import (
	"strings"
	"testing"
)

func altEntry(loc string, alts ...SitemapAlternate) SitemapEntry {
	return SitemapEntry{Loc: loc, ChangeFreq: "weekly", Priority: "0.7", Alternates: alts}
}

func buildOne(t *testing.T, e SitemapEntry) string {
	t.Helper()
	out, err := BuildSitemap([]SitemapEntry{e})
	if err != nil {
		t.Fatalf("BuildSitemap: %v", err)
	}
	return out
}

func TestBuildSitemapSingleLangDropsGroup(t *testing.T) {

	one := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/about"}))
	if strings.Contains(one, "xhtml") {
		t.Fatalf("单语言不应输出 xhtml:link\n%s", one)
	}

	withDefault := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/about"},
		SitemapAlternate{Lang: "x-default", Href: "https://e.com/about"}))
	if strings.Contains(withDefault, "xhtml") {
		t.Fatalf("唯一语言 + x-default 仍属单语言，不应输出分组\n%s", withDefault)
	}

	bare := buildOne(t, SitemapEntry{Loc: "https://e.com/about", ChangeFreq: "weekly", Priority: "0.7"})
	if bare != withDefault {
		t.Fatalf("单语言分组字节应退化为无互指形态\n无互指:\n%s\n有互指:\n%s", bare, withDefault)
	}
	if !strings.Contains(withDefault, "<loc>https://e.com/about</loc>") {
		t.Fatalf("url 节点本身必须保留\n%s", withDefault)
	}
}

func TestBuildSitemapMultiLangOrderIndependent(t *testing.T) {
	asc := []SitemapAlternate{
		{Lang: "en-US", Href: "https://e.com/en-US/about"},
		{Lang: "ja-JP", Href: "https://e.com/ja-JP/about"},
		{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
	}
	sorted := buildOne(t, altEntry("https://e.com/about", asc...))

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

	if again := buildOne(t, altEntry("https://e.com/about", asc...)); again != sorted {
		t.Fatal("同输入两次输出不一致")
	}
}

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

	if got := strings.Count(multi, "https://e.com/zh-CN/about"); got != 2 {
		t.Fatalf("x-default 应复用已列出的语言 URL，zh-CN 应出现 2 次，实际 %d\n%s", got, multi)
	}

	noDefault := buildOne(t, altEntry("https://e.com/about",
		SitemapAlternate{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
		SitemapAlternate{Lang: "en-US", Href: "https://e.com/en-US/about"},
	))
	if strings.Contains(noDefault, "x-default") {
		t.Fatalf("无默认标记时不应输出 x-default\n%s", noDefault)
	}
}

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
