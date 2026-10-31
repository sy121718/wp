package builder

import (
	"strings"
	"testing"
)

func hreflangTestSEO() SEO { return SEO{Title: "关于我们", Description: "描述"} }

func TestBuildSEOHeadHreflangLanguageGate(t *testing.T) {
	if out := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", nil); strings.Contains(out, "hreflang") {
		t.Fatalf("无互指时不应输出 hreflang:\n%s", out)
	}
	one := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", []Alternate{
		{Lang: "zh-CN", Href: "/about", Default: true},
	})
	if strings.Contains(one, "hreflang") {
		t.Fatalf("单语言（哪怕标了 Default）不应输出 hreflang:\n%s", one)
	}

	dirty := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", []Alternate{
		{Lang: "", Href: "/x"}, {Lang: "en-US", Href: ""}, {Lang: "zh-CN", Href: "/about"},
	})
	if strings.Contains(dirty, "hreflang") {
		t.Fatalf("非法互指应被忽略且不计入语言数:\n%s", dirty)
	}
}

func TestBuildSEOHeadHreflangOrderAndXDefault(t *testing.T) {
	alts := []Alternate{
		{Lang: "zh-CN", Href: "/zh-CN/about"},
		{Lang: "ja-JP", Href: "/ja-JP/about"},
		{Lang: "en-US", Href: "/en-US/about", Default: true},
	}
	out := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", alts)
	for _, want := range []string{
		"hreflang=\"en-US\" href=\"/en-US/about\"",
		"hreflang=\"ja-JP\" href=\"/ja-JP/about\"",
		"hreflang=\"zh-CN\" href=\"/zh-CN/about\"",
		"hreflang=\"x-default\" href=\"/en-US/about\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少互指标签 %s:\n%s", want, out)
		}
	}
	iEn := strings.Index(out, "hreflang=\"en-US\"")
	iJa := strings.Index(out, "hreflang=\"ja-JP\"")
	iZh := strings.Index(out, "hreflang=\"zh-CN\"")
	iXd := strings.Index(out, "hreflang=\"x-default\"")
	if iEn < 0 || iJa < 0 || iZh < 0 || iXd < 0 || iEn > iJa || iJa > iZh || iZh > iXd {
		t.Fatalf("互指应为 en-US < ja-JP < zh-CN < x-default:\n%s", out)
	}
	if n := strings.Count(out, "hreflang=\"x-default\""); n != 1 {
		t.Fatalf("x-default 应恰好 1 条，实际 %d:\n%s", n, out)
	}

	if again := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", alts); again != out {
		t.Fatal("同输入两次输出不一致")
	}
}

func TestBuildSEOHeadHreflangXDefaultFirstWins(t *testing.T) {
	out := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", []Alternate{
		{Lang: "zh-CN", Href: "/zh-CN/about", Default: true},
		{Lang: "en-US", Href: "/en-US/about", Default: true},
	})
	if n := strings.Count(out, "hreflang=\"x-default\""); n != 1 {
		t.Fatalf("x-default 应恰好 1 条，实际 %d:\n%s", n, out)
	}
	if !strings.Contains(out, "hreflang=\"x-default\" href=\"/zh-CN/about\"") {
		t.Fatalf("x-default 应取输入顺序的第一条 Default:\n%s", out)
	}
}
