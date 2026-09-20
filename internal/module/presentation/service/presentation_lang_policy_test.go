package presentationservice

// presentation_lang_policy_test.go — 发布口径的语言取数（审计 I18N-02）。
//
// 自动发布实例这条链上有两处消费站点语言清单，处置方式不同但**取数口径必须相同**：
//   - publishAllLangs：本次发布到底要上线哪几种语言。读不到就整批中止，绝不降级成
//     「只发布默认语言 + 推进实例指针 + 标记收敛」——那会让其余语言的线上产物静默停在
//     旧字节，且再没有任何东西会去发现它；
//   - batchConverged：恢复流程的收敛判定。读不到时本轮不处置（记 Error 后返回 true），
//     但不能用降级集合 —— 用它会把「每种语言都有账本行」缩成「只查默认语言那一行」，
//     于是基于错误的事实宣称收敛。
//
// 端到端测不出这两处的差别：编译期的发布口径冻结（pipeline.SiteCompileOptions）会让
// 语言表读不到时的构建必然失败，两条路径在数据库上留下同一个状态。所以要在这里钉住
// 取数口本身 —— 它一被改回宽松口径，本文件立刻红。

import (
	"context"
	"errors"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
)

// stubLangErrProject 语言清单读取失败的最小工程服务。
type stubLangErrProject struct {
	projectcontract.ProjectService
	langs []string
	err   error
}

func (s stubLangErrProject) EnabledLangs(ctx context.Context, projectID string) ([]string, error) {
	return s.langs, s.err
}

// TestPublishLangsOfForbidsFallback 发布口径：读不到即失败，且不返回回退集合。
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
