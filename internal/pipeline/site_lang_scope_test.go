package pipeline

import (
	"context"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

func TestLocaleViewBatchScopeIgnoresPublished(t *testing.T) {

	never := func(string) bool { return false }

	alts, links := LocaleView(LocaleViewInput{
		Ctx: context.Background(), ProjectID: "p1",
		LogicalPath: "/about", Lang: "zh-CN",
		TargetLangs: []string{"zh-CN", "en-US"},
		Published:   never,
	})
	if len(alts) != 2 || len(links) != 2 {
		t.Fatalf("批次口径下互指应覆盖本批次两种语言，实际 alts=%+v links=%+v", alts, links)
	}
	if alts[0].Lang != "zh-CN" || !alts[0].Default {
		t.Fatalf("默认语言条目的 Default 标记决定 x-default，实际 %+v", alts[0])
	}
	if alts[1].Lang != "en-US" || alts[1].Href != "/en/about" {
		t.Fatalf("非默认语言互指应指向其站点路径，实际 %+v", alts[1])
	}
}

type stubLangProject struct {
	projectcontract.ProjectService
	langs []string
}

func (s stubLangProject) EnabledLangs(ctx context.Context, projectID string) ([]string, error) {
	return s.langs, nil
}

func (s stubLangProject) DefaultLocale(ctx context.Context, projectID string) (string, error) {
	return s.langs[0], nil
}

func (s stubLangProject) SiteLangURLMode(context.Context, string) (string, error) { return "", nil }

func TestLocaleViewPageScopeKeepsPublishedFilter(t *testing.T) {

	project := stubLangProject{langs: []string{"zh-CN", "en-US"}}
	never := func(string) bool { return false }

	alts, links := LocaleView(LocaleViewInput{
		Ctx: context.Background(), Project: project, ProjectID: "p1",
		LogicalPath: "/about", Lang: "zh-CN", Published: never,
	})
	if len(alts) != 0 || len(links) != 0 {
		t.Fatalf("page 口径下未发布的语言不进互指，实际 alts=%+v links=%+v", alts, links)
	}

	published := func(p string) bool { return p == "/en/about" }
	alts, links = LocaleView(LocaleViewInput{
		Ctx: context.Background(), Project: project, ProjectID: "p1",
		LogicalPath: "/about", Lang: "zh-CN", Published: published,
	})
	if len(alts) != 2 || len(links) != 2 {
		t.Fatalf("已发布语言应进互指，实际 alts=%+v links=%+v", alts, links)
	}
	if !alts[0].Default || alts[1].Lang != "en-US" {
		t.Fatalf("默认语言 / 非默认语言的互指标记不对：%+v", alts)
	}
}
