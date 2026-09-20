package unit

// i18n02_lang_freeze_test.go — 发布冻结语言表（审计 I18N-02）。
//
// 缺陷现象：站点语言清单读取失败时，EnabledLangs 降级为「只有默认语言一种」。
// 草稿 / 预览口径下这是可接受的（作者不该因为一次读库失败就打不开页面），
// 发布口径下不可接受：产物照常产出、发布回执照常写成功，而线上其余语言停在旧字节，
// 唯一的痕迹是一行会滚走的日志。
//
// 这里钉住三件事：
//  1. 等级策略本身：Forbidden 在「读取失败 / 清单为空 / 无工程上下文」三种情况都失败，
//     且失败原因可被 errors.Is 识别（调用方要能判断「重试有意义」）；
//  2. 发布口径的判定：SiteCompileOptions 在发布口径下冻结语言表、读不到即失败；
//     预览口径（不注入发布面查询）仍是可见告警回退 —— 两种口径的差别只有这一条判据；
//  3. 冻结是**一次读取**：产物与 Manifest 不能各读一次语言表（两次读之间清单被改，
//     产物与 Manifest 就会各说各话）。

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

// stubLangProject 只回答语言相关两问的最小工程服务；调用次数用于验证「只读一次」。
type stubLangProject struct {
	projectcontract.ProjectService
	langs []string
	err   error
	// calls 记录 EnabledLangs 被调用的次数（指针接收者之外用值接收者也可读，
	// 调用方传 *stubLangProject 以便观察）。
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

// TestResolveSiteLangsVisibleFallsBack 预览口径：读取失败降级为默认语言一种，不报错。
func TestResolveSiteLangsVisibleFallsBack(t *testing.T) {
	project := &stubLangProject{err: errors.New("数据库不可用")}
	langs, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackVisible)
	if err != nil {
		t.Fatalf("预览口径不应报错，实际 %v", err)
	}
	if len(langs) != 1 || langs[0] != i18n.GetDefaultLang() {
		t.Fatalf("预览口径应降级为默认语言一种，实际 %v", langs)
	}
	// EnabledLangs 必须与 Visible 逐字等价（它是既有调用方的入口）。
	if got := pipeline.EnabledLangs(context.Background(), project, "p1"); len(got) != 1 || got[0] != i18n.GetDefaultLang() {
		t.Fatalf("EnabledLangs 应与 Visible 口径一致，实际 %v", got)
	}
}

// TestResolveSiteLangsForbiddenFailsOnReadError 发布口径：读取失败即失败。
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

// TestResolveSiteLangsForbiddenFailsOnEmptyTable 空清单与读取失败同等级：
// 空清单会让发布退化成单语言并覆盖线上其它语言，同样不能继续。
func TestResolveSiteLangsForbiddenFailsOnEmptyTable(t *testing.T) {
	project := &stubLangProject{langs: []string{}}
	if _, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackForbidden); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("空清单在发布口径下应失败，实际 %v", err)
	}
	// 预览口径不变：空清单仍回退默认语言一种（作者页面照常打开）。
	langs, err := pipeline.ResolveSiteLangs(context.Background(), project, "p1", pipeline.LangFallbackVisible)
	if err != nil || len(langs) != 1 {
		t.Fatalf("空清单在预览口径下应回退默认语言一种，实际 langs=%v err=%v", langs, err)
	}
}

// TestResolveSiteLangsForbiddenWithoutProjectContext 缺工程上下文同样是「拿不到语言表」。
func TestResolveSiteLangsForbiddenWithoutProjectContext(t *testing.T) {
	if _, err := pipeline.ResolveSiteLangs(context.Background(), nil, "p1", pipeline.LangFallbackForbidden); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("未注入工程服务时应失败，实际 %v", err)
	}
	project := &stubLangProject{langs: []string{"zh-CN"}}
	if _, err := pipeline.ResolveSiteLangs(context.Background(), project, "  ", pipeline.LangFallbackForbidden); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("工程 ID 为空时应失败，实际 %v", err)
	}
}

// TestSiteCompileOptionsFreezeLangTableByScope 发布口径冻结语言表、预览口径允许回退。
//
// 判据是「注入了发布面查询 + 有逻辑路径 + 有工程」，而不是调用方名字：
// 四个真实调用点在这个判据下的取值见 publishLangScope 注释。
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
			Project: broken, // 不注入 RoutePublished = 预览（不看发布面）
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

// TestLocaleViewFrozenLangsDriveAlternates 冻结的语言表就是互指的唯一来源：
// 传进来的 SiteLangs 直接决定产物里的互指，不再回读工程服务。
func TestLocaleViewFrozenLangsDriveAlternates(t *testing.T) {
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })

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

// TestLocaleViewForbiddenPolicyEmitsNoAlternates 发布口径下语言表不可读且调用方漏了
// 上游检查时：宁可**没有互指**，也不产出「只指默认语言」的假互指。
func TestLocaleViewForbiddenPolicyEmitsNoAlternates(t *testing.T) {
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })

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

// TestSiteCompileOptionsRecordFrozenLangsIntoUsage 冻结的语言表随 ctx 回到内核
// （写进 Manifest 的那条链路，端到端见 publisher 用例）。
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
	// 预览口径（不注入收集器）不应 panic：装配层拿不到收集器是正常路径。
	if _, err := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: project, RoutePublished: func(string) bool { return true },
	}, pipeline.SiteCompileParams{
		Ctx: context.Background(), ProjectID: "p1", Lang: "zh-CN", LogicalPath: "/about",
	}); err != nil {
		t.Fatalf("无收集器时不应失败: %v", err)
	}
}

// TestPublisherBuildFailsWhenPublishLangTableUnreadable 端到端：语言表读不到的发布
// 构建必须失败（内核返回错误、不落任何产物），而不是产出「只有默认语言」的产物。
func TestPublisherBuildFailsWhenPublishLangTableUnreadable(t *testing.T) {
	p, _, _, root := newPublisherEnv(t, pipeline.WithCompile(func(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {
		// 复刻 page / presentation 的装配：发布口径注入发布面查询。
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
	// 失败必须是「什么都没产出」：留下一个半截产物目录，回收/审计都会把它当成真产物。
	if entries, rerr := os.ReadDir(filepath.Join(root, "artifacts")); rerr == nil && len(entries) > 0 {
		t.Fatalf("构建失败时不应落盘产物，实际有 %d 个：%v", len(entries), entries)
	}
}
