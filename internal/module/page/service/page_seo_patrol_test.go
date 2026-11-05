package pageservice

import (
	publish "go_wp/pkg/publish"
	"os"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/seo/compliance"
)

func TestLangRulesOfDefaultLangHasNoPrefix(t *testing.T) {
	rule := publish.NewLangURLRule(true, false, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	if len(got) != 2 {
		t.Fatalf("应推导出两条语言规则，实际 %d", len(got))
	}
	if got[0].Code != "zh-CN" || got[0].Prefix != "" {
		t.Errorf("默认语言应为无前缀，实际 %+v", got[0])
	}
	if got[1].Code != "en-US" || got[1].Prefix != "/en" {
		t.Errorf("en-US 的前缀应为 /en（短码由 LangURLRule 决定），实际 %+v", got[1])
	}
}

func TestLangRulesOfPrefixDefault(t *testing.T) {
	rule := publish.NewLangURLRule(true, true, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	if len(got) != 2 || got[0].Prefix != "/zh" || got[1].Prefix != "/en" {
		t.Fatalf("全前缀模式下两条语言都应带前缀，实际 %+v", got)
	}
}

func TestLangRulesOfUnseparated(t *testing.T) {
	rule := publish.NewLangURLRule(false, false, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	for _, l := range got {
		if l.Prefix != "" {
			t.Fatalf("未分路径模式下不应有任何前缀，实际 %+v", l)
		}
	}
}

func TestLangRulesOfShortCodeOverride(t *testing.T) {

	rule := publish.NewLangURLRule(true, true, "zh-CN", map[string]string{"zh-CN": "cn"})
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	if got[0].Prefix != "/cn" {
		t.Fatalf("短码覆盖为 cn 时 zh-CN 的前缀应为 /cn，实际 %+v", got[0])
	}
	if got[1].Prefix != "/en" {
		t.Fatalf("未覆盖的语言应走内置映射，实际 %+v", got[1])
	}
}

func TestLangRulesOfInvalidLangSkipped(t *testing.T) {
	rule := publish.NewLangURLRule(true, false, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "a/b"})
	if len(got) != 1 || got[0].Code != "zh-CN" {
		t.Fatalf("非法语言码应被跳过，实际 %+v", got)
	}
}

func TestInspectArtifactSEOReadsArtifactFromStore(t *testing.T) {
	t.Setenv("WP_SITE_BASE_URL", "https://shop.test/store")
	store := &publish.LocalStore{Root: t.TempDir()}

	html := []byte("<!DOCTYPE html><html lang=\"zh-CN\"><head><title>关于我们</title>" +
		"<link rel=\"canonical\" href=\"https://shop.test/store/old-about\">" +
		"<script type=\"application/ld+json\">{\"@type\":\"WebPage\",\"name\":\"关于我们\",\"url\":\"https://shop.test/store/old-about\"}</script>" +
		"</head><body></body></html>")
	art, err := publish.NewArtifact(html, &publish.Manifest{
		ManifestSchemaVersion: publish.ManifestSchemaVersion,
		CompilerVersion:       "test",
		SourceID:              "page-1",
		SourceType:            publish.SourceTypePage,
		CanonicalPath:         "/about",
		Lang:                  "zh-CN",
	})
	if err != nil {
		t.Fatalf("构造产物失败: %v", err)
	}
	if _, err = store.PutArtifact(art); err != nil {
		t.Fatalf("落盘产物失败: %v", err)
	}

	svc := &Service{store: store}
	rep, err := svc.inspectArtifactSEO(art.Hash, "/about", "zh-CN", false, nil)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	var hit bool
	for _, f := range rep.Findings {
		if f.Rule == compliance.RuleCanonicalPathMismatch {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("从存储读出的产物必须被真的检查（零结论往往意味着压根没读到字节），实际 %+v", rep.Findings)
	}
	if rep.ArtifactHash != art.Hash {
		t.Fatalf("结论必须锚定到产物哈希，实际 %q", rep.ArtifactHash)
	}
}

func TestInspectArtifactSEOEmptyHash(t *testing.T) {
	svc := &Service{store: &publish.LocalStore{Root: t.TempDir()}}
	if _, err := svc.inspectArtifactSEO("", "/about", "zh-CN", false, nil); err == nil {
		t.Fatal("空 hash 应返回错误（静默返回空结论 = 假绿）")
	}
}

func TestToSEOFindingItemKeepsEvidence(t *testing.T) {
	in := compliance.Finding{
		Rule: compliance.RuleCanonicalMultiple, Level: compliance.LevelError,
		URL: "/about", Lang: "en-US", ArtifactHash: "hash-1",
		Evidence: "head 中有 2 条 canonical", Expect: "只能有一条 canonical",
	}
	got := toSEOFindingItem(in)
	want := pagedto.SEOFindingItemResp{
		URL: "/about", Lang: "en-US", ArtifactHash: "hash-1",
		Rule: compliance.RuleCanonicalMultiple, Level: "error",
		Evidence: "head 中有 2 条 canonical", Expect: "只能有一条 canonical",
	}
	if got != want {
		t.Fatalf("结论到 DTO 的映射丢字段：\n实际 %+v\n期望 %+v", got, want)
	}
}

func TestBuildPathRunsSEOInspection(t *testing.T) {
	src, err := os.ReadFile("page_publish.go")
	if err != nil {
		t.Fatalf("读取 page_publish.go 失败: %v", err)
	}

	if n := strings.Count(string(src), "inspectBuiltArtifact("); n < 2 {
		t.Fatalf("构建 / 发布路径应至少调用两次构建期 SEO 校验（Build + Publish 复构建），实际 %d 次", n)
	}
}
