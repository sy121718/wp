package unit

// publisher_lang_test.go — 语言维度与确定性（多语言 P2，docs/06-D §2.3/§4.2 D2）。
//
// 核心不变量：同一 Page Document + 同一 BuildContext（含 lang）+ 同一 Registry
// + 同一 Compiler → 字节相同的 Artifact；不同 lang → 独立 hash、独立路径。

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

// buildWithLang 用指定语言构建并返回 (hash, artifact)。
func buildWithLang(t *testing.T, p *pipeline.Publisher, store *pipeline.LocalStore, lang, path string) (string, *pipeline.Artifact) {
	t.Helper()
	ctx := context.Background()
	if _, err := p.SaveDraftInput("page-1", 0, pipeline.Draft{Path: path, Lang: lang, DocJSON: []byte(docV2)}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	hash, err := p.Build(ctx, "page-1", 1)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	a, err := store.GetArtifact(pipeline.ArtifactLocator(hash))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return hash, a
}

// TestPublisherLangDimension 同一文档两个语言：hash 不同、Manifest 记录语言、路径带前缀。
func TestPublisherLangDimension(t *testing.T) {
	p, store, _, _ := newPublisherEnv(t)
	zhHash, zhArt := buildWithLang(t, p, store, "zh-CN", "/zh-CN/about")

	// 同文档换语言：内核记录整体重建（语言是记录维度）。
	if _, err := p.SaveDraftInput("page-1", 1, pipeline.Draft{Path: "/en-US/about", Lang: "en-US", DocJSON: []byte(docV2)}); err != nil {
		t.Fatalf("保存 en-US 草稿失败: %v", err)
	}
	enHash, err := p.Build(context.Background(), "page-1", 2)
	if err != nil {
		t.Fatalf("构建 en-US 失败: %v", err)
	}
	if zhHash == enHash {
		t.Fatal("不同语言产物 hash 不应相同（lang 必须进 Manifest）")
	}
	enArt, err := store.GetArtifact(pipeline.ArtifactLocator(enHash))
	if err != nil {
		t.Fatalf("读取 en-US 产物失败: %v", err)
	}

	if zhArt.Manifest.Lang != "zh-CN" || zhArt.CanonicalPath != "/zh-CN/about" {
		t.Fatalf("zh-CN 产物 Manifest 错误: lang=%q path=%q", zhArt.Manifest.Lang, zhArt.CanonicalPath)
	}
	if enArt.Manifest.Lang != "en-US" || enArt.CanonicalPath != "/en-US/about" {
		t.Fatalf("en-US 产物 Manifest 错误: lang=%q path=%q", enArt.Manifest.Lang, enArt.CanonicalPath)
	}
	// Manifest JSON 必须真的带 lang 字段（D2：回滚/校验靠它区分语言）。
	if !strings.Contains(string(enArt.Entries["manifest.json"]), "\"lang\":\"en-US\"") {
		t.Fatalf("manifest.json 未包含 lang 字段: %s", enArt.Entries["manifest.json"])
	}
}

// TestPublisherDeterminismSameLang 同语言两次构建字节完全一致（确定性不变量）。
func TestPublisherDeterminismSameLang(t *testing.T) {
	p, store, _, _ := newPublisherEnv(t)
	firstHash, firstArt := buildWithLang(t, p, store, "zh-CN", "/zh-CN/about")

	// 同版本重复构建（编译器升级 / 复构建校验路径）。
	secondHash, err := p.Build(context.Background(), "page-1", 1)
	if err != nil {
		t.Fatalf("重复构建失败: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("同输入两次构建 hash 不一致: %s vs %s", firstHash, secondHash)
	}
	secondArt, err := store.GetArtifact(pipeline.ArtifactLocator(secondHash))
	if err != nil {
		t.Fatalf("读取第二次产物失败: %v", err)
	}
	if string(firstArt.Entries["index.html"]) != string(secondArt.Entries["index.html"]) {
		t.Fatal("同输入两次构建 index.html 字节不一致")
	}
	if string(firstArt.Entries["manifest.json"]) != string(secondArt.Entries["manifest.json"]) {
		t.Fatal("同输入两次构建 manifest.json 字节不一致")
	}
}

// TestPublisherI18nDependency 依赖提供者把 i18n 词条 revision 写进 Manifest。
func TestPublisherI18nDependency(t *testing.T) {
	p, store, _, _ := newPublisherEnv(t, pipeline.WithDependencies(
		func(context.Context, pipeline.BuildInput) []pipeline.Dependency {
			return []pipeline.Dependency{pipeline.I18NDependency("i18n-rev-7")}
		}))
	hash, err := p.Build(context.Background(), func() string {
		if _, e := p.SaveDraftInput("page-1", 0, pipeline.Draft{Path: "/zh-CN/about", Lang: "zh-CN", DocJSON: []byte(docV2)}); e != nil {
			t.Fatalf("保存草稿失败: %v", e)
		}
		return "page-1"
	}(), 1)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	a, err := store.GetArtifact(pipeline.ArtifactLocator(hash))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if len(a.Manifest.Dependencies) != 1 {
		t.Fatalf("依赖条目数错误: %+v", a.Manifest.Dependencies)
	}
	d := a.Manifest.Dependencies[0]
	if d.Kind != pipeline.DependencyKindI18N || d.Key != pipeline.I18NDependencyKey || d.Revision != "i18n-rev-7" {
		t.Fatalf("i18n 依赖条目错误: %+v", d)
	}
}
