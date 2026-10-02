package pipeline

// site_lang_scope_test.go — hreflang 互指的判定依据（审计 SEO-026）。
//
// 缺陷现象：自动发布的详情页**首次**发布的产物缺 hreflang，紧接着重建一次才有。
// 成因不是链接拼错，而是判据取错了层：互指按「某个访问路径是否已发布」过滤，而这个
// 状态要等该语言逐语言结案才产生 —— 构建发生在结案之前，同批语言一律被判定为
// 「没发布」，互指全部落空。
//
// 这里钉住的是**判据本身**：批次发布方给的语言集合是构建输入，它非空时
// 互指就不该再受任何「已发布状态」回调影响；而手工 Page（没有批次概念）的口径
// 一点没变。

import (
	"context"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
)

// TestLocaleViewBatchScopeIgnoresPublished 批次口径下，published 回调一律不参与判定。
func TestLocaleViewBatchScopeIgnoresPublished(t *testing.T) {

	// 「首发布」的复刻：这一批语言的账本行都还没写，访问面上除了本语言什么都没有。
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

// stubLangProject 只回答语言相关两问的最小工程服务；其余方法嵌入接口占位
// （本用例不会走到，真走到就是 panic，比静默返回零值更容易发现）。
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

// SiteLangURLMode 工程级语言 URL 方案覆盖。本用例不涉及该维度，返回空串 =
// 「该工程未配置」→ 解析回退全局默认方案（与实测行为一致）。
func (s stubLangProject) SiteLangURLMode(context.Context, string) (string, error) { return "", nil }

// TestLocaleViewPageScopeKeepsPublishedFilter 没有批次时（手工 Page）退回访问面口径，
// I18N-021 的过滤一条不少 —— 未发布的语言不进互指，当前语言一定保留。
func TestLocaleViewPageScopeKeepsPublishedFilter(t *testing.T) {

	project := stubLangProject{langs: []string{"zh-CN", "en-US"}}
	never := func(string) bool { return false }

	// 访问面上什么都没有：只剩当前语言一条 → 凑不满两种语言，不出互指。
	// 这条同时反证了批次口径的必要性 —— 首发布正是这个状态，若按它判定就永远没有 hreflang。
	alts, links := LocaleView(LocaleViewInput{
		Ctx: context.Background(), Project: project, ProjectID: "p1",
		LogicalPath: "/about", Lang: "zh-CN", Published: never,
	})
	if len(alts) != 0 || len(links) != 0 {
		t.Fatalf("page 口径下未发布的语言不进互指，实际 alts=%+v links=%+v", alts, links)
	}

	// 当前语言之外的语言已发布 → 两条互指都在（page 口径不变的正向对照）。
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
