package seo

import (
	"strings"
	"testing"
)

func TestBuildSitemapAlternates(t *testing.T) {
	entries := []SitemapEntry{
		{
			Loc: "https://e.com/zh-CN/about", ChangeFreq: "weekly", Priority: "0.7",
			Alternates: []SitemapAlternate{
				{Lang: "x-default", Href: "https://e.com/zh-CN/about"},
				{Lang: "zh-CN", Href: "https://e.com/zh-CN/about"},
				{Lang: "en-US", Href: "https://e.com/en-US/about"},
			},
		},
		{Loc: "https://e.com/en-US/about", ChangeFreq: "weekly", Priority: "0.7"},
	}
	out, err := BuildSitemap(entries)
	if err != nil {
		t.Fatalf("BuildSitemap: %v", err)
	}
	if !strings.Contains(out, `xmlns:xhtml="http://www.w3.org/1999/xhtml"`) {
		t.Fatalf("缺少 xhtml 命名空间声明:\n%s", out)
	}
	if strings.Count(out, "<xhtml:link") != 3 {
		t.Fatalf("应 3 条互指（en-US/zh-CN/x-default），实际 %d\n%s", strings.Count(out, "<xhtml:link"), out)
	}

	if strings.Index(out, `hreflang="en-US"`) > strings.Index(out, `hreflang="zh-CN"`) {
		t.Fatalf("互指未按语言码升序:\n%s", out)
	}
	if strings.Index(out, `hreflang="zh-CN"`) > strings.Index(out, `hreflang="x-default"`) {
		t.Fatalf("x-default 应最后输出:\n%s", out)
	}
	again, _ := BuildSitemap(entries)
	if again != out {
		t.Fatal("同输入两次输出不一致")
	}
}

func TestBuildSitemapNoAlternatesKeepsBytes(t *testing.T) {
	out, err := BuildSitemap(EntriesFromPaths("https://e.com", []string{"/about"}))
	if err != nil {
		t.Fatalf("BuildSitemap: %v", err)
	}
	if strings.Contains(out, "xhtml") {
		t.Fatalf("单语言 sitemap 不应出现 xhtml 命名空间:\n%s", out)
	}
}
