package unit

// i18n02_manifest_locale_test.go — 语言环境事实进 Manifest（审计 I18N-02）。
//
// 三项事实都只在「构建之后」才确定，所以都在内核装配 Manifest 时取走：
//   · Dir —— 书写方向，与 HTML 的 <html dir> 同源（builder.DirAttr）；
//   · SiteLangs —— 本次发布冻结的站点语言表；
//   · TranslationMisses —— 内容译文缺失统计（策略 + 候选数 + 缺失数）。
//
// 为什么必须是 Manifest 而不是日志：发布验收要能**机器判定**「这次产物里有多少字段
// 回退成了原文」，日志滚走了、也没法参与判定 —— 本次审计点名不许把日志告警当质量检查。

import (
	"context"
	"testing"

	"go_wp/internal/pipeline"
)

// missCounter 复刻取词器的缺失计数（真实实现是 *i18n.ContentTranslator）。
type missCounter struct{ misses int64 }

func (m missCounter) Misses() int64 { return m.misses }

// buildWithUsage 用给定编译函数构建一个产物并取回 Manifest。
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

// TestManifestDirFollowsLocaleDirection dir 与 HTML 同源：RTL 落字节、LTR 省略。
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

// TestManifestSiteLangsRecordsFrozenTable 冻结的站点语言表进 Manifest。
func TestManifestSiteLangsRecordsFrozenTable(t *testing.T) {
	m := buildWithUsage(t, "ar", "/ar/about", func(u *pipeline.CompileUsage) {
		u.SetSiteLangs([]string{"zh-CN", "en-US", "ar"})
	})
	if len(m.SiteLangs) != 3 || m.SiteLangs[2] != "ar" {
		t.Fatalf("Manifest 应记录本次发布冻结的语言表，实际 %v", m.SiteLangs)
	}
	// 未冻结（预览口径的编译函数）时字段必须缺省 —— 不能出现「空但存在」的假事实。
	if m := buildWithUsage(t, "zh-CN", "/about", nil); m.SiteLangs != nil {
		t.Fatalf("未冻结时不应写 siteLangs，实际 %v", m.SiteLangs)
	}
}

// TestManifestTranslationMisses 缺译统计进 Manifest，且「没接入内容翻译」与
// 「接入了但零缺失」必须区分得开。
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

	// 接入了内容翻译但一条都没缺：仍要记录（candidates>0），否则「零缺失」与
	// 「没接入」在 Manifest 里长得一样，发布验收分不清。
	zero := buildWithUsage(t, "en-US", "/en/about", func(u *pipeline.CompileUsage) {
		u.RecordContentTranslation(5, missCounter{misses: 0})
	})
	if zero.TranslationMisses == nil || zero.TranslationMisses.Misses != 0 || zero.TranslationMisses.Candidates != 5 {
		t.Fatalf("零缺失也必须记录，实际 %+v", zero.TranslationMisses)
	}

	// 默认语言 / 单语言站点不接入内容翻译：字段缺省。
	if m := buildWithUsage(t, "zh-CN", "/about", nil); m.TranslationMisses != nil {
		t.Fatalf("未接入内容翻译时不应写统计，实际 %+v", m.TranslationMisses)
	}
}

// TestManifestTranslationMissesReadAfterCompile 缺失数必须在**渲染结束后**读：
// 记录时若立刻取值，写进 Manifest 的会是一份恒为 0 的假事实。
func TestManifestTranslationMissesReadAfterCompile(t *testing.T) {
	counter := &lateMissCounter{}
	m := buildWithUsage(t, "en-US", "/en/about", func(u *pipeline.CompileUsage) {
		u.RecordContentTranslation(7, counter)
		// 记录之后「渲染」才发生：缺失数在记录那一刻还是 0。
		counter.misses = 4
	})
	if m.TranslationMisses == nil || m.TranslationMisses.Misses != 4 {
		t.Fatalf("缺失数应在编译结束后读取，实际 %+v", m.TranslationMisses)
	}
}

// lateMissCounter 可变的缺失计数（模拟渲染期累加）。
type lateMissCounter struct{ misses int64 }

func (m *lateMissCounter) Misses() int64 { return m.misses }
