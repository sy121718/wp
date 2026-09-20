package pageservice

// page_lang_policy_test.go — 发布口径与草稿口径的语言取数分界（审计 I18N-02）。
//
// 本条 finding 的整改要求是「发布冻结语言表，清单读取失败直接失败」。落到模块侧的难点
// 不在实现，而在**逐个调用点判断口径**：同一份语言清单，草稿路径读不到可以回退（作者
// 不该因为一次读库抖动存不了草稿），发布/重建路径读不到就必须失败（回退成「只有默认
// 语言一种」会让该做的重建与 sitemap 分组静默少做）。
//
// 因此这里钉的不是「某个方法返回什么」，而是**两个取数口的分界本身**：
//   - publishLangsOf：读不到即报错，且不返回任何集合（回退集合会被上游当成真值继续发布）；
//   - enabledLangsOf：唯一调用方是站点路径占位（page_draft.go 的建页/保存草稿），保留回退。
//
// 端到端（真实 PG）为什么不足以钉住它：编译期已经有一道发布口径冻结
// （pipeline.SiteCompileOptions），语言表读不到时 RebuildStale 里的 s.Build 必然失败 ——
// 于是「跳过整页」与「按默认语言一种尝试一次注定失败的构建」在数据库里留下的是同一个
// 状态，端到端用例测不出差别。差别在「有没有拿着降级后的语言集去做事」，只能在这一层钉。

import (
	"context"
	"errors"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

// stubLangErrProject 语言清单读取失败的最小工程服务（其余方法嵌入接口占位）。
type stubLangErrProject struct {
	projectcontract.ProjectService
	langs []string
	err   error
}

func (s stubLangErrProject) EnabledLangs(ctx context.Context, projectID string) ([]string, error) {
	return s.langs, s.err
}

// TestPublishLangsOfForbidsFallback 发布/重建口径：语言清单读不到即失败，不给回退集合。
func TestPublishLangsOfForbidsFallback(t *testing.T) {
	svc := &Service{project: stubLangErrProject{err: errors.New("数据库不可用")}}
	langs, err := svc.publishLangsOf(context.Background(), "p1")
	if !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("发布口径读不到语言清单必须失败（RebuildStale 的语言遍历 / sitemap 分组都靠它），实际 err=%v", err)
	}
	if langs != nil {
		t.Fatalf("失败时不得返回语言集合：回退集合会被上游当成真值继续发布，实际 %v", langs)
	}

	// 空清单与读取失败同等级：空清单同样会让重建与 sitemap 退化成单语言。
	svc = &Service{project: stubLangErrProject{langs: []string{}}}
	if _, err = svc.publishLangsOf(context.Background(), "p1"); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("空清单在发布口径下应失败，实际 err=%v", err)
	}

	// 正常读取：原样返回清单（不得顺手补默认语言或去重排序）。
	svc = &Service{project: stubLangErrProject{langs: []string{"zh-CN", "ar"}}}
	langs, err = svc.publishLangsOf(context.Background(), "p1")
	if err != nil || len(langs) != 2 || langs[1] != "ar" {
		t.Fatalf("正常读取应原样返回语言清单，实际 langs=%v err=%v", langs, err)
	}
}

// TestEnabledLangsOfKeepsDraftFallback 草稿口径：唯一调用方是站点路径占位，保留可见回退。
//
// 这条不是「顺便测一下旧行为」，而是钉住分界：一旦有人把 enabledLangsOf 也改成严格口径，
// 建页/保存草稿会在一次读库抖动时直接失败（作者视角是「编辑保存不了」）；反过来若有人把
// publishLangsOf 改回宽松，上面那条用例会红。
func TestEnabledLangsOfKeepsDraftFallback(t *testing.T) {
	svc := &Service{project: stubLangErrProject{err: errors.New("数据库不可用")}}
	langs := svc.enabledLangsOf(context.Background(), "p1")
	if len(langs) != 1 || langs[0] != i18n.GetDefaultLang() {
		t.Fatalf("草稿口径（路径占位）应降级为默认语言一种，实际 %v", langs)
	}
}
