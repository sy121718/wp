package unit

// seo_head_alternates_test.go — 构建期 hreflang 互指（多语言 P3，docs/06-D §5）。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
)

// TestBuildSEOHeadAlternates hreflang 输出：≥2 语言才输出，语言码升序、x-default 最后。
func TestBuildSEOHeadAlternates(t *testing.T) {
	seo := builder.SEO{Title: "关于我们", Description: "描述"}
	base := builder.BuildSEOHead(seo, "/zh-CN/about", "关于我们", "描述", nil)
	if strings.Contains(base, "hreflang") {
		t.Fatalf("无互指时不应输出 hreflang:\n%s", base)
	}

	// 单条互指（自指）无意义：不输出。
	one := builder.BuildSEOHead(seo, "/zh-CN/about", "", "", []builder.Alternate{{Lang: "zh-CN", Href: "/zh-CN/about"}})
	if strings.Contains(one, "hreflang") {
		t.Fatalf("单条互指不应输出 hreflang:\n%s", one)
	}

	alts := []builder.Alternate{
		{Lang: "zh-CN", Href: "/zh-CN/about", Default: true},
		{Lang: "ja-JP", Href: "/ja-JP/about"},
		{Lang: "en-US", Href: "/en-US/about"},
	}
	out := builder.BuildSEOHead(seo, "/zh-CN/about", "", "", alts)
	for _, want := range []string{
		`hreflang="zh-CN" href="/zh-CN/about"`,
		`hreflang="en-US" href="/en-US/about"`,
		`hreflang="ja-JP" href="/ja-JP/about"`,
		`hreflang="x-default" href="/zh-CN/about"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少互指标签 %s:\n%s", want, out)
		}
	}
	if strings.Index(out, `hreflang="en-US"`) > strings.Index(out, `hreflang="ja-JP"`) ||
		strings.Index(out, `hreflang="ja-JP"`) > strings.Index(out, `hreflang="zh-CN"`) {
		t.Fatalf("互指未按语言码升序:\n%s", out)
	}
	if strings.Index(out, `hreflang="zh-CN"`) > strings.Index(out, `hreflang="x-default"`) {
		t.Fatalf("x-default 应最后输出:\n%s", out)
	}
	// 确定性：同输入两次字节一致。
	if again := builder.BuildSEOHead(seo, "/zh-CN/about", "", "", alts); again != out {
		t.Fatal("同输入两次输出不一致")
	}
	// 空语言/空 href 的互指被忽略（不会输出非法标签）。
	dirty := builder.BuildSEOHead(seo, "/zh-CN/about", "", "", []builder.Alternate{
		{Lang: "", Href: "/x"}, {Lang: "en-US", Href: ""}, {Lang: "zh-CN", Href: "/zh-CN/about"},
	})
	if strings.Contains(dirty, "hreflang") {
		t.Fatalf("非法互指应被忽略:\n%s", dirty)
	}
}
