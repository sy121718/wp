package unit

import (
	"context"
	"testing"

	"go_wp/internal/pipeline"
)

type missCounter struct{ misses int64 }

func (m missCounter) Misses() int64 { return m.misses }

func buildWithUsage(t *testing.T, lang, path string, fn func(*pipeline.CompileUsage)) pipeline.Manifest {
	t.Helper()
	p, store, _, _ := newPublisherEnv(t, pipeline.WithCompile(func(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {
		u := pipeline.CompileUsageFromContext(ctx)
		if u == nil {
			t.Fatal("构建口径必须把依赖线索收集器放进 ctx（装配层靠它把构建期事实带回内核）")
		}
		if fn != nil {
			fn(u)
		}
		return []byte("<!DOCTYPE html><html></html>"), nil
	}))
	if _, err := p.SaveDraftInput("page-1", 0, pipeline.Draft{Path: path, Lang: lang, DocJSON: []byte(docV1)}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	hash, err := p.Build(context.Background(), "page-1", 1)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	a, err := store.GetArtifact(pipeline.ArtifactLocator(hash))
	if err != nil {
		t.Fatalf("读取产物失败: %v", err)
	}
	return a.Manifest
}

func TestManifestDirFollowsLocaleDirection(t *testing.T) {
	if m := buildWithUsage(t, "ar", "/ar/about", nil); m.Dir != "rtl" || m.Lang != "ar" {
		t.Fatalf("RTL 语言应记录 dir=rtl，实际 dir=%q lang=%q", m.Dir, m.Lang)
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		if m := buildWithUsage(t, lang, "/about", nil); m.Dir != "" {
			t.Fatalf("LTR 语言不应记录 dir（缺省即 LTR，写了会让全部存量产物换 hash），实际 %q", m.Dir)
		}
	}
}

func TestManifestSiteLangsRecordsFrozenTable(t *testing.T) {
	m := buildWithUsage(t, "ar", "/ar/about", func(u *pipeline.CompileUsage) {
		u.SetSiteLangs([]string{"zh-CN", "en-US", "ar"})
	})
	if len(m.SiteLangs) != 3 || m.SiteLangs[2] != "ar" {
		t.Fatalf("Manifest 应记录本次发布冻结的语言表，实际 %v", m.SiteLangs)
	}

	if m := buildWithUsage(t, "zh-CN", "/about", nil); m.SiteLangs != nil {
		t.Fatalf("未冻结时不应写 siteLangs，实际 %v", m.SiteLangs)
	}
}

func TestManifestTranslationMisses(t *testing.T) {
	m := buildWithUsage(t, "en-US", "/en/about", func(u *pipeline.CompileUsage) {
		u.RecordContentTranslation(12, missCounter{misses: 3})
	})
	if m.TranslationMisses == nil {
		t.Fatal("接入内容翻译时必须记录缺失统计")
	}
	if m.TranslationMisses.Policy != pipeline.TranslationPolicyFallback {
		t.Fatalf("字段策略应显式声明为回退原文，实际 %q", m.TranslationMisses.Policy)
	}
	if m.TranslationMisses.Candidates != 12 || m.TranslationMisses.Misses != 3 {
		t.Fatalf("统计应为 candidates=12 misses=3，实际 %+v", m.TranslationMisses)
	}

	zero := buildWithUsage(t, "en-US", "/en/about", func(u *pipeline.CompileUsage) {
		u.RecordContentTranslation(5, missCounter{misses: 0})
	})
	if zero.TranslationMisses == nil || zero.TranslationMisses.Misses != 0 || zero.TranslationMisses.Candidates != 5 {
		t.Fatalf("零缺失也必须记录，实际 %+v", zero.TranslationMisses)
	}

	if m := buildWithUsage(t, "zh-CN", "/about", nil); m.TranslationMisses != nil {
		t.Fatalf("未接入内容翻译时不应写统计，实际 %+v", m.TranslationMisses)
	}
}

func TestManifestTranslationMissesReadAfterCompile(t *testing.T) {
	counter := &lateMissCounter{}
	m := buildWithUsage(t, "en-US", "/en/about", func(u *pipeline.CompileUsage) {
		u.RecordContentTranslation(7, counter)

		counter.misses = 4
	})
	if m.TranslationMisses == nil || m.TranslationMisses.Misses != 4 {
		t.Fatalf("缺失数应在编译结束后读取，实际 %+v", m.TranslationMisses)
	}
}

type lateMissCounter struct{ misses int64 }

func (m *lateMissCounter) Misses() int64 { return m.misses }
