package unit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

type stubLangProject struct {
	projectcontract.ProjectService
	langs []string
	err   error

	calls *int
}

func (s *stubLangProject) EnabledLangs(ctx context.Context, projectID string) ([]string, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.langs, s.err
}

func (s *stubLangProject) DefaultLocale(ctx context.Context, projectID string) (string, error) {
	return i18n.GetDefaultLang(), nil
}

func (s *stubLangProject) SiteLangURLMode(context.Context, string) (string, error) { return "", nil }

func TestResolveSiteLangsVisibleFallsBack(t *testing.T) {
	project := &stubLangProject{err: errors.New("数据库不可用")}
	langs, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackVisible)
	if err != nil {
		t.Fatalf("预览口径不应报错，实际 %v", err)
	}
	if len(langs) != 1 || langs[0] != i18n.GetDefaultLang() {
		t.Fatalf("预览口径应降级为默认语言一种，实际 %v", langs)
	}

	if got := pipeline.EnabledLangs(context.Background(), project, "p1"); len(got) != 1 || got[0] != i18n.GetDefaultLang() {
		t.Fatalf("EnabledLangs 应与 Visible 口径一致，实际 %v", got)
	}
}

func TestResolveSiteLangsForbiddenFailsOnReadError(t *testing.T) {
	project := &stubLangProject{err: errors.New("连接被拒绝")}
	langs, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackForbidden)
	if err == nil {
		t.Fatalf("发布口径下读取失败必须报错，实际返回 %v", langs)
	}
	if !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("失败原因应可被 ErrLangTableUnavailable 识别（调用方据此判断重试是否有意义），实际 %v", err)
	}
	if langs != nil {
		t.Fatalf("失败时不应返回任何语言集合（降级集合会被上层当成真值），实际 %v", langs)
	}
}

func TestResolveSiteLangsForbiddenFailsOnEmptyTable(t *testing.T) {
	project := &stubLangProject{langs: []string{}}
	if _, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackForbidden); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("空清单在发布口径下应失败，实际 %v", err)
	}

	langs, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackVisible)
	if err != nil || len(langs) != 1 {
		t.Fatalf("空清单在预览口径下应回退默认语言一种，实际 langs=%v err=%v", langs, err)
	}
}

func TestResolveSiteLangsForbiddenWithoutProjectContext(t *testing.T) {
	if _, err := pipeline.ResolveSiteLangs(context.Background(), nil, "p1", pipeline.LangFallbackForbidden); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("未注入工程服务时应失败，实际 %v", err)
	}
	project := &stubLangProject{langs: []string{"zh-CN"}}
	if _, err := pipeline.ResolveSiteLangs(context.Background(), project, "  ", pipeline.LangFallbackForbidden); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("工程 ID 为空时应失败，实际 %v", err)
	}
}

func TestSiteCompileOptionsFreezeLangTableByScope(t *testing.T) {
	broken := &stubLangProject{err: errors.New("数据库不可用")}
	published := func(string) bool { return true }

	t.Run("发布口径：读不到语言表即失败", func(t *testing.T) {
		_, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
			Project: broken, RoutePublished: published,
		}, pipeline.SiteCompileParams{
			Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/about",
		})
		if !errors.Is(err, pipeline.ErrLangTableUnavailable) {
			t.Fatalf("发布口径必须失败，实际 err=%v", err)
		}
	})

	t.Run("预览口径：回退为默认语言一种且不报错", func(t *testing.T) {
		_, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
			Project: broken,
		}, pipeline.SiteCompileParams{
			Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/about",
		})
		if err != nil {
			t.Fatalf("预览口径不应报错，实际 %v", err)
		}
	})

	t.Run("自动发布实例预览：注入发布面查询但没有逻辑路径", func(t *testing.T) {
		_, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
			Project: broken, RoutePublished: published,
		}, pipeline.SiteCompileParams{
			Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "",
		})
		if err != nil {
			t.Fatalf("无逻辑路径的编译（实体草稿预览）不应报错，实际 %v", err)
		}
	})

	t.Run("发布口径只读一次语言表", func(t *testing.T) {
		calls := 0
		project := &stubLangProject{langs: []string{"zh-CN", "en-US"}, calls: &calls}
		if _, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
			Project: project, RoutePublished: published,
		}, pipeline.SiteCompileParams{
			Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/about",
		}); err != nil {
			t.Fatalf("正常读取不应失败: %v", err)
		}
		if calls != 1 {
			t.Fatalf("冻结后不应再回读语言表（产物与 Manifest 必须依据同一份），实际读取 %d 次", calls)
		}
	})
}

func TestLocaleViewFrozenLangsDriveAlternates(t *testing.T) {

	calls := 0
	project := &stubLangProject{err: errors.New("不该被读到"), calls: &calls}
	alts, links := pipeline.LocaleView(pipeline.LocaleViewInput{
		Ctx: context.Background(), Project: project, ProjectID: "p1",
		LogicalPath: "/about", Lang: "zh-CN",
		SiteLangs:    []string{"zh-CN", "en-US"},
		LangFallback: pipeline.LangFallbackForbidden,
		Published:    func(string) bool { return true },
	})
	if calls != 0 {
		t.Fatalf("已冻结语言表时不应回读工程服务，实际读取 %d 次", calls)
	}
	if len(alts) != 2 || len(links) != 2 || alts[1].Href != "/en/about" {
		t.Fatalf("互指应来自冻结的语言表，实际 alts=%+v links=%+v", alts, links)
	}
}

func TestLocaleViewForbiddenPolicyEmitsNoAlternates(t *testing.T) {

	project := &stubLangProject{err: errors.New("数据库不可用")}
	alts, links := pipeline.LocaleView(pipeline.LocaleViewInput{
		Ctx: context.Background(), Project: project, ProjectID: "p1",
		LogicalPath: "/about", Lang: "zh-CN",
		LangFallback: pipeline.LangFallbackForbidden,
		Published:    func(string) bool { return true },
	})
	if len(alts) != 0 || len(links) != 0 {
		t.Fatalf("语言表不可读时不应产出互指，实际 alts=%+v links=%+v", alts, links)
	}
}

func TestSiteCompileOptionsRecordFrozenLangsIntoUsage(t *testing.T) {
	usage := &pipeline.CompileUsage{}
	ctx := pipeline.WithCompileUsage(context.Background(), usage)
	project := &stubLangProject{langs: []string{"zh-CN", "ar"}}
	if _, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: project, RoutePublished: func(string) bool { return true },
	}, pipeline.SiteCompileParams{
		Ctx: ctx, ProjectID: "p1", Lang: "ar", LogicalPath: "/about",
	}); err != nil {
		t.Fatalf("正常读取不应失败: %v", err)
	}
	got := usage.SiteLangs()
	if len(got) != 2 || got[0] != "zh-CN" || got[1] != "ar" {
		t.Fatalf("冻结语言表应写进依赖线索收集器，实际 %v", got)
	}

	if _, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: project, RoutePublished: func(string) bool { return true },
	}, pipeline.SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/about",
	}); err != nil {
		t.Fatalf("无收集器时不应失败: %v", err)
	}
}

func TestPublisherBuildFailsWhenPublishLangTableUnreadable(t *testing.T) {
	p, _, _, root := newPublisherEnv(t, pipeline.WithCompile(func(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {

		if _, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
			Project:        &stubLangProject{err: errors.New("数据库不可用")},
			RoutePublished: func(string) bool { return true },
		}, pipeline.SiteCompileParams{
			Ctx: ctx, ProjectID: "p1", Lang: in.Lang, LogicalPath: in.Path,
		}); err != nil {
			return nil, err
		}
		return []byte("<!DOCTYPE html><html></html>"), nil
	}))

	if _, err := p.SaveDraftInput("page-1", 0, pipeline.Draft{Path: "/about", Lang: "zh-CN", DocJSON: []byte(docV1)}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}
	hash, err := p.Build(context.Background(), "page-1", 1)
	if err == nil {
		t.Fatalf("语言表不可读时构建必须失败，实际产出产物 %s", hash)
	}
	if !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("失败原因应可识别为语言表不可用，实际 %v", err)
	}
	if hash != "" {
		t.Fatalf("失败时不应返回产物哈希，实际 %s", hash)
	}

	if entries, rerr := os.ReadDir(filepath.Join(root, "artifacts")); rerr == nil && len(entries) > 0 {
		t.Fatalf("构建失败时不应落盘产物，实际有 %d 个：%v", len(entries), entries)
	}
}
