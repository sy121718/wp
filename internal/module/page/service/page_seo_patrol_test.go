package pageservice

// page_seo_patrol_test.go — 构建期 SEO 校验接入点的就近单测（不碰库、不读盘）。
//
// 这里钉的是两处「错了不会崩、只会误报」的判断：语言前缀的推导、结论到 DTO 的映射。
// 误报的代价被低估得很厉害 —— 一个天天喊狼来了的巡检等于不存在，而它绿着的时候
// 没人会去查它到底在看什么。

import (
	"os"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/pipeline"
	"go_wp/internal/seo/compliance"
)

// TestLangRulesOfDefaultLangHasNoPrefix 默认语言不带前缀时前缀必须是空串。
func TestLangRulesOfDefaultLangHasNoPrefix(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
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

// TestLangRulesOfPrefixDefault 全语言带前缀时默认语言也有前缀。
func TestLangRulesOfPrefixDefault(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, true, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	if len(got) != 2 || got[0].Prefix != "/zh" || got[1].Prefix != "/en" {
		t.Fatalf("全前缀模式下两条语言都应带前缀，实际 %+v", got)
	}
}

// TestLangRulesOfUnseparated 未开启语言分路径时全部语言共用逻辑路径（前缀为空）。
func TestLangRulesOfUnseparated(t *testing.T) {
	rule := pipeline.NewLangURLRule(false, false, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	for _, l := range got {
		if l.Prefix != "" {
			t.Fatalf("未分路径模式下不应有任何前缀，实际 %+v", l)
		}
	}
}

// TestLangRulesOfShortCodeOverride URL 短码可被配置覆盖 —— 前缀必须跟随映射点而不是语言码。
func TestLangRulesOfShortCodeOverride(t *testing.T) {
	// 用「全语言带前缀」模式：默认语言不带前缀时它本来就不占短码，
	// 拿它去验短码映射会把两个规则混在一起（那两件事各有各的用例）。
	rule := pipeline.NewLangURLRule(true, true, "zh-CN", map[string]string{"zh-CN": "cn"})
	got := langRulesOf(rule, []string{"zh-CN", "en-US"})
	if got[0].Prefix != "/cn" {
		t.Fatalf("短码覆盖为 cn 时 zh-CN 的前缀应为 /cn，实际 %+v", got[0])
	}
	if got[1].Prefix != "/en" {
		t.Fatalf("未覆盖的语言应走内置映射，实际 %+v", got[1])
	}
}

// TestLangRulesOfInvalidLangSkipped 非法语言码不产生规则（不替它编一个前缀）。
func TestLangRulesOfInvalidLangSkipped(t *testing.T) {
	rule := pipeline.NewLangURLRule(true, false, "zh-CN", nil)
	got := langRulesOf(rule, []string{"zh-CN", "a/b"})
	if len(got) != 1 || got[0].Code != "zh-CN" {
		t.Fatalf("非法语言码应被跳过，实际 %+v", got)
	}
}

// TestInspectArtifactSEOReadsArtifactFromStore 校验器必须真的把产物字节读出来，
// 而不是在「读不到」时静默返回一份空结论。
//
// 这一条守的是最危险的一种失效：GetArtifact 失败 / 键名写错（如误取 Entries["index.htm"]）
// 时，结论会变成「零 findings」—— 报告全绿，而它其实什么都没看。用真实的本地产物存储
// （临时目录）跑一次，缺陷产物必须命中。
func TestInspectArtifactSEOReadsArtifactFromStore(t *testing.T) {
	store := &pipeline.LocalStore{Root: t.TempDir()}
	// 这里用解释型字符串而不是反引号原文串：HTML 里没有需要转义的换行，
	// 而反引号原文串在同一行里更容易被格式化工具改坏（测试只需要一份确定的字节）。
	html := []byte("<!DOCTYPE html><html lang=\"zh-CN\"><head><title>关于我们</title>" +
		"<link rel=\"canonical\" href=\"/old-about\">" +
		"<script type=\"application/ld+json\">{\"@type\":\"WebPage\",\"name\":\"关于我们\",\"url\":\"/old-about\"}</script>" +
		"</head><body></body></html>")
	art, err := pipeline.NewArtifact(html, &pipeline.Manifest{
		ManifestSchemaVersion: pipeline.ManifestSchemaVersion,
		CompilerVersion:       "test",
		SourceID:              "page-1",
		SourceType:            pipeline.SourceTypePage,
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

// TestInspectArtifactSEOEmptyHash 空 hash 是调用方的错误，必须显式失败而不是返回空结论。
func TestInspectArtifactSEOEmptyHash(t *testing.T) {
	svc := &Service{store: &pipeline.LocalStore{Root: t.TempDir()}}
	if _, err := svc.inspectArtifactSEO("", "/about", "zh-CN", false, nil); err == nil {
		t.Fatal("空 hash 应返回错误（静默返回空结论 = 假绿）")
	}
}

// TestToSEOFindingItemKeepsEvidence 结论 → DTO 必须四要素齐备（URL / 规则 / 证据 / ArtifactHash）。
func TestToSEOFindingItemKeepsEvidence(t *testing.T) {
	in := compliance.Finding{
		Rule: compliance.RuleCanonicalMultiple, Level: compliance.LevelError,
		URL: "/about", Lang: "en-US", ArtifactHash: "hash-1",
		Evidence: "head 中有 2 条 canonical", Expect: "只能有一条 canonical",
	}
	got := toSEOFindingItem(in)
	want := pagedto.SEOFindingItem{
		URL: "/about", Lang: "en-US", ArtifactHash: "hash-1",
		Rule: compliance.RuleCanonicalMultiple, Level: "error",
		Evidence: "head 中有 2 条 canonical", Expect: "只能有一条 canonical",
	}
	if got != want {
		t.Fatalf("结论到 DTO 的映射丢字段：\n实际 %+v\n期望 %+v", got, want)
	}
}

// TestBuildPathRunsSEOInspection 静态门禁：构建 / 发布路径必须调用构建期 SEO 校验。
//
// 为什么用静态扫描而不是行为断言：校验的产物是**日志**，本包没有日志捕获设施；
// 更要紧的是这类「顺手删掉一行调用」的失效完全静默 —— 站点照常发布，只是巡检从此
// 不再看新增的产物，报告缺一整块而没人发现。同一个仓库里已有同形的先例
// （public/test/architecture/tx_boundary_scan_test.go 扫事务边界）。
//
// 它是启发式：重命名或挪位置会让它失败，这正好逼着改的人同时更新这处门禁。
// 它只能证明「调用还在源码里」，证明不了运行时确实跑了 —— 那由
// TestInspectArtifactSEOReadsArtifactFromStore 与实际的构建日志共同兜住。
func TestBuildPathRunsSEOInspection(t *testing.T) {
	src, err := os.ReadFile("page_publish.go")
	if err != nil {
		t.Fatalf("读取 page_publish.go 失败: %v", err)
	}
	// 两处：Build（手工构建）与 Publish 的「暂存落后于站点级状态」复构建分支。
	if n := strings.Count(string(src), "inspectBuiltArtifact("); n < 2 {
		t.Fatalf("构建 / 发布路径应至少调用两次构建期 SEO 校验（Build + Publish 复构建），实际 %d 次", n)
	}
}
