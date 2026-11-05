package unit

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/seo/compliance"
)

func renderArtifact(t *testing.T, lang string, seo builder.SEO, alternates []builder.Alternate) []byte {
	t.Helper()
	head := builder.BuildSEOHead(seo, seo.Canonical, seo.Title, seo.Description, "首页", alternates)
	doc, err := builder.RenderDocument(&builder.CompiledPage{
		Lang:            lang,
		Title:           seo.Title,
		MetaDescription: seo.Description,
		SEOHead:         head,
		HTML:            "<p>正文</p>",
	})
	if err != nil {
		t.Fatalf("渲染产物失败: %v", err)
	}
	return []byte(doc)
}

func inspectBuilt(t *testing.T, a compliance.Artifact) *compliance.Report {
	t.Helper()
	rep := compliance.Inspect(a)
	for _, f := range rep.Findings {
		t.Logf("结论：%s [%s] %s —— %s", f.Rule, f.Level, f.Evidence, f.Expect)
	}
	return rep
}

func TestBuiltArtifactPassesCompliance(t *testing.T) {
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "关于我们", Description: "关于我们的介绍", Canonical: "/about",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/about", Lang: "zh-CN", ArtifactHash: "h-single", HTML: html,
	})
	if len(rep.Findings) != 0 {
		t.Fatalf("真实构建产物不应命中任何规则，实际 %d 条", len(rep.Findings))
	}
	if !rep.Index.Index || !rep.Index.Follow {
		t.Fatalf("未声明 robots 时索引策略应为默认 index,follow，实际 %+v", rep.Index)
	}
}

func TestBuiltArtifactHomePagePassesCompliance(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.test")
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "示例站点", Description: "站点描述", Canonical: "/", SchemaType: "website",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/", Lang: "zh-CN", ArtifactHash: "h-home", HTML: html,
		SiteBaseURL: "https://shop.test",
	})
	if len(rep.Findings) != 0 {
		t.Fatalf("首页产物不应命中任何规则，实际 %d 条", len(rep.Findings))
	}
}

func TestBuiltArtifactCrossSiteCanonicalReported(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.test")
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "关于我们", Canonical: "https://other.test/about",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/about", Lang: "zh-CN", HTML: html, SiteBaseURL: "https://shop.test",
	})
	if len(rep.Findings) != 1 || rep.Findings[0].Rule != compliance.RuleCanonicalExternal {
		t.Fatalf("真实构建产物跨域 canonical 应只报告跨域，实际 %+v", rep.Findings)
	}
}

func TestBuiltArtifactHreflangPassesCompliance(t *testing.T) {
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "关于我们", Description: "描述", Canonical: "/about",
	}, []builder.Alternate{
		{Lang: "zh-CN", Href: "/about", Default: true},
		{Lang: "en-US", Href: "/en/about"},
	})
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/about", Lang: "zh-CN", ArtifactHash: "h-alt", HTML: html,
		Langs: []compliance.LangRule{{Code: "zh-CN"}, {Code: "en-US", Prefix: "/en"}},
	})
	if len(rep.Findings) != 0 {
		t.Fatalf("多语言产物不应命中任何规则，实际 %d 条", len(rep.Findings))
	}
}

func TestBuiltArtifactNoindexWithoutSitemapPasses(t *testing.T) {
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "内部页", RobotsIndex: "noindex", RobotsFollow: "nofollow", Canonical: "/internal",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/internal", Lang: "zh-CN", ArtifactHash: "h-noindex", HTML: html,
	})
	if len(rep.Findings) != 0 {
		t.Fatalf("未收录的 noindex 页面不应命中任何规则，实际 %d 条", len(rep.Findings))
	}
	if rep.Index.Index || rep.Index.Follow {
		t.Fatalf("noindex,nofollow 应解析为不收录不跟踪，实际 %+v", rep.Index)
	}
	if !rep.Index.Explicit || rep.Index.Raw != "noindex,nofollow" {
		t.Fatalf("显式声明的 robots 必须可在报告里复核，实际 %+v", rep.Index)
	}
}

func TestBuiltArtifactNoindexInSitemapReported(t *testing.T) {
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "内部页", RobotsIndex: "noindex", Canonical: "/internal",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/internal", Lang: "zh-CN", ArtifactHash: "h-noindex-2",
		HTML: html, SitemapListed: true,
	})
	if len(rep.Findings) != 1 || rep.Findings[0].Rule != compliance.RuleSitemapNoindexConflict {
		t.Fatalf("应只报出 sitemap.noindex-conflict，实际 %+v", rep.Findings)
	}
	if rep.Findings[0].ArtifactHash != "h-noindex-2" {
		t.Fatalf("结论必须带 ArtifactHash 才能复核，实际 %+v", rep.Findings[0])
	}
}

func TestBuiltArtifactRealWorldDefectIsCaught(t *testing.T) {
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "关于我们", Canonical: "/old-about",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/about", Lang: "zh-CN", ArtifactHash: "h-defect", HTML: html,
	})
	var found bool
	for _, f := range rep.Findings {
		if f.Rule == compliance.RuleCanonicalPathMismatch {
			found = true
			if !strings.Contains(f.Evidence, "/old-about") || !strings.Contains(f.Evidence, "/about") {
				t.Fatalf("证据必须同时给出实际值与期望值：%+v", f)
			}
		}
	}
	if !found {
		t.Fatalf("canonical 指向别的路径必须被报出，实际 %+v", rep.Findings)
	}

	if !rep.OK() {
		t.Fatalf("warn 级结论不应把报告判成不合规：%+v", rep.Findings)
	}
}
