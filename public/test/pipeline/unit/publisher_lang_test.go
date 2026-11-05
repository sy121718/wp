package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"strings"
	"testing"

	"go_wp/internal/pipeline"
)

func buildWithLang(t *testing.T, p *pipeline.Publisher, store *publish.LocalStore, lang, path string) (string, *publish.Artifact) {
	t.Helper()
	ctx := context.Background()
	if _, err := p.SaveDraftInput("page-1", 0, pipeline.Draft{Path: path, Lang: lang, DocJSON: []byte(docV2)}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	hash, err := p.Build(ctx, "page-1", 1)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	a, err := store.GetArtifact(publish.ArtifactLocator(hash))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return hash, a
}

func TestPublisherLangDimension(t *testing.T) {
	p, store, _, _ := newPublisherEnv(t)
	zhHash, zhArt := buildWithLang(t, p, store, "zh-CN", "/zh-CN/about")

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
	enArt, err := store.GetArtifact(publish.ArtifactLocator(enHash))
	if err != nil {
		t.Fatalf("读取 en-US 产物失败: %v", err)
	}

	if zhArt.Manifest.Lang != "zh-CN" || zhArt.CanonicalPath != "/zh-CN/about" {
		t.Fatalf("zh-CN 产物 Manifest 错误: lang=%q path=%q", zhArt.Manifest.Lang, zhArt.CanonicalPath)
	}
	if enArt.Manifest.Lang != "en-US" || enArt.CanonicalPath != "/en-US/about" {
		t.Fatalf("en-US 产物 Manifest 错误: lang=%q path=%q", enArt.Manifest.Lang, enArt.CanonicalPath)
	}

	if !strings.Contains(string(enArt.Entries["manifest.json"]), "\"lang\":\"en-US\"") {
		t.Fatalf("manifest.json 未包含 lang 字段: %s", enArt.Entries["manifest.json"])
	}
}

func TestPublisherDeterminismSameLang(t *testing.T) {
	p, store, _, _ := newPublisherEnv(t)
	firstHash, firstArt := buildWithLang(t, p, store, "zh-CN", "/zh-CN/about")

	secondHash, err := p.Build(context.Background(), "page-1", 1)
	if err != nil {
		t.Fatalf("重复构建失败: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("同输入两次构建 hash 不一致: %s vs %s", firstHash, secondHash)
	}
	secondArt, err := store.GetArtifact(publish.ArtifactLocator(secondHash))
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

func TestPublisherI18nDependency(t *testing.T) {
	p, store, _, _ := newPublisherEnv(t, pipeline.WithDependencies(
		func(context.Context, pipeline.BuildInput) []publish.Dependency {
			return []publish.Dependency{publish.I18NDependency("i18n-rev-7")}
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
	a, err := store.GetArtifact(publish.ArtifactLocator(hash))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	if len(a.Manifest.Dependencies) != 1 {
		t.Fatalf("依赖条目数错误: %+v", a.Manifest.Dependencies)
	}
	d := a.Manifest.Dependencies[0]
	if d.Kind != publish.DependencyKindI18N || d.Key != publish.I18NDependencyKey || d.Revision != "i18n-rev-7" {
		t.Fatalf("i18n 依赖条目错误: %+v", d)
	}
}
