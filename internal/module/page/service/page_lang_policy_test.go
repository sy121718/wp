package pageservice

import (
	"context"
	"errors"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

type stubLangErrProject struct {
	projectcontract.ProjectService
	langs []string
	err   error
}

func (s stubLangErrProject) EnabledLangs(ctx context.Context, projectID string) ([]string, error) {
	return s.langs, s.err
}

func TestPublishLangsOfForbidsFallback(t *testing.T) {
	svc := &Service{project: stubLangErrProject{err: errors.New("数据库不可用")}}
	langs, err := svc.publishLangsOf(context.Background(), "p1")
	if !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("发布口径读不到语言清单必须失败（RebuildStale 的语言遍历 / sitemap 分组都靠它），实际 err=%v", err)
	}
	if langs != nil {
		t.Fatalf("失败时不得返回语言集合：回退集合会被上游当成真值继续发布，实际 %v", langs)
	}

	svc = &Service{project: stubLangErrProject{langs: []string{}}}
	if _, err = svc.publishLangsOf(context.Background(), "p1"); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("空清单在发布口径下应失败，实际 err=%v", err)
	}

	svc = &Service{project: stubLangErrProject{langs: []string{"zh-CN", "ar"}}}
	langs, err = svc.publishLangsOf(context.Background(), "p1")
	if err != nil || len(langs) != 2 || langs[1] != "ar" {
		t.Fatalf("正常读取应原样返回语言清单，实际 langs=%v err=%v", langs, err)
	}
}

func TestEnabledLangsOfKeepsDraftFallback(t *testing.T) {
	svc := &Service{project: stubLangErrProject{err: errors.New("数据库不可用")}}
	langs := svc.enabledLangsOf(context.Background(), "p1")
	if len(langs) != 1 || langs[0] != i18n.GetDefaultLang() {
		t.Fatalf("草稿口径（路径占位）应降级为默认语言一种，实际 %v", langs)
	}
}
