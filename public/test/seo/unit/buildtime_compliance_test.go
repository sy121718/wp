package unit

// seo_compliance_build_test.go — 校验器与**真实构建产物**的对账（审计 SEO-01 前半段）。
//
// 这一个文件只回答一个问题：合规校验器对 builder 实际产出的形状理解对不对？
//
// 为什么必须单独测这一层：internal/seo/compliance 自己的用例用手写 HTML —— 手写样例
// 永远按校验器的假设写，所以它们只能证明「规则按我以为的形状工作」，证明不了
//「我以为的形状就是构建期真正产出的形状」。形状对不上的后果是假警（天天报「语种/路径
// 不一致」而线上一切正常），而假警会让整个巡检失去意义。
//
// 因此这里用 builder.BuildSEOHead + builder.RenderDocument（与构建期同一份模板骨架）
// 产出真产物，再断言零结论。

import (
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/seo/compliance"
)

// renderArtifact 渲染一份与构建期同源的产物字节。
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

// inspectBuilt 跑一次校验并返回结论。
func inspectBuilt(t *testing.T, a compliance.Artifact) *compliance.Report {
	t.Helper()
	rep := compliance.Inspect(a)
	for _, f := range rep.Findings {
		t.Logf("结论：%s [%s] %s —— %s", f.Rule, f.Level, f.Evidence, f.Expect)
	}
	return rep
}

// TestBuiltArtifactPassesCompliance 单语言页面：真实产物必须一条结论都不产生。
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

// TestBuiltArtifactHomePagePassesCompliance 首页 + 站点基址：站点级 @graph 不应触发误报。
//
// 首页会额外输出 Organization / WebSite / 面包屑节点，是结构化数据形状最复杂的一种；
// 若主实体的识别写错（把站点节点当成页面节点），这里会立刻报「标题不一致」。
func TestBuiltArtifactHomePagePassesCompliance(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.test")
	html := renderArtifact(t, "zh-CN", builder.SEO{
		Title: "示例站点", Description: "站点描述", Canonical: "/", SchemaType: "website",
	}, nil)
	rep := inspectBuilt(t, compliance.Artifact{
		URL: "/", Lang: "zh-CN", ArtifactHash: "h-home", HTML: html,
	})
	if len(rep.Findings) != 0 {
		t.Fatalf("首页产物不应命中任何规则，实际 %d 条", len(rep.Findings))
	}
}

// TestBuiltArtifactHreflangPassesCompliance 多语言互指：真实产物满足自指与 x-default。
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

// TestBuiltArtifactNoindexWithoutSitemapPasses noindex 尚未进 sitemap 时不算矛盾。
//
// 这一条钉住「sitemap 收录」这个事实来自调用方：校验器自己判断不了站点级文件状态，
// 拿不到就说不知道，而不是默认它已经进 sitemap 天天报警。
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

// TestBuiltArtifactNoindexInSitemapReported 同一份产物一旦进了收录清单就是矛盾。
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

// TestBuiltArtifactRealWorldDefectIsCaught 用真实 builder 产出**有缺陷**的产物：校验器必须报出来。
//
// 这是「失败能力验证」的常驻版本：把 canonical 指到别的路径（作者改了 URL 但没重建），
// 校验器要能指出「产物宣称自己住在另一个地址」。若这条用例绿着而实现被改回「不校验」，
// 说明用例没有失败能力 —— 那种用例只是装饰。
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
	// warn 与 error 的区别要钉住：canonical 指向别处可能是作者有意的跨域声明，
	// 因此它不把整份报告判成不合规 —— 等级只描述「要不要立刻看」，不描述阻断与否。
	if !rep.OK() {
		t.Fatalf("warn 级结论不应把报告判成不合规：%+v", rep.Findings)
	}
}
