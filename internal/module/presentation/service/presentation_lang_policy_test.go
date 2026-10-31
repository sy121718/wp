package presentationservice

import (
	"context"
	"errors"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
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
		t.Fatalf("发布口径读不到语言清单必须失败（整批发布与收敛判定都靠它），实际 err=%v", err)
	}
	if langs != nil {
		t.Fatalf("失败时不得返回语言集合，实际 %v", langs)
	}

	svc = &Service{project: stubLangErrProject{langs: []string{}}}
	if _, err = svc.publishLangsOf(context.Background(), "p1"); !errors.Is(err, pipeline.ErrLangTableUnavailable) {
		t.Fatalf("空清单在发布口径下应失败，实际 err=%v", err)
	}

	svc = &Service{project: stubLangErrProject{langs: []string{"zh-CN", "en-US", "ar"}}}
	langs, err = svc.publishLangsOf(context.Background(), "p1")
	if err != nil || len(langs) != 3 {
		t.Fatalf("正常读取应原样返回语言清单，实际 langs=%v err=%v", langs, err)
	}
}
