package builder

// seo_head_hreflang_test.go — 构建期 hreflang 互指的包内回归（审计 docs/06-D §15.5 第 4 条）。
//
// 与 public/test/builder/unit/seo_head_alternates_test.go 同义，但落在包内：后者所在的
// 测试包还依赖 page 模块，跨线编译故障会连坐跑不起来，而这三条（至少两种语言才输出、
// 语言码升序、x-default 只取第一条且固定最后）是 BuildSEOHead 自身的契约，
// 应当在本包内独立可验证。

import (
	"strings"
	"testing"
)

func hreflangTestSEO() SEO { return SEO{Title: "关于我们", Description: "描述"} }

// TestBuildSEOHeadHreflangLanguageGate 语言数量闸门：无互指与单语言都不输出。
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
	// 空语言 / 空 href 一律忽略，不参与语言计数。
	dirty := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", []Alternate{
		{Lang: "", Href: "/x"}, {Lang: "en-US", Href: ""}, {Lang: "zh-CN", Href: "/about"},
	})
	if strings.Contains(dirty, "hreflang") {
		t.Fatalf("非法互指应被忽略且不计入语言数:\n%s", dirty)
	}
}

// TestBuildSEOHeadHreflangOrderAndXDefault 多语言：语言码升序、x-default 固定最后、字节确定。
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
	// 确定性：同输入两次字节一致。
	if again := BuildSEOHead(hreflangTestSEO(), "/about", "", "", "", alts); again != out {
		t.Fatal("同输入两次输出不一致")
	}
}

// TestBuildSEOHeadHreflangXDefaultFirstWins 多个 Default 时只认输入顺序的第一条（固定最后输出）。
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
