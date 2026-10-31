package pipeline

import (
	"context"
	"errors"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

type stubLangModeProject struct {
	projectcontract.ProjectService
	modes map[string]string
	err   error
}

func (s stubLangModeProject) SiteLangURLMode(_ context.Context, projectID string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.modes[projectID], nil
}

func withGlobalDefaultMode(t *testing.T, mode i18n.SiteLangURLMode) {
	t.Helper()
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{SiteLangURLMode: string(mode)}, nil
	})
	t.Cleanup(func() {
		i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) { return i18n.RuntimeValues{}, nil })
		i18n.SetValueLoader(nil)
	})
}

func TestSiteLangURLModeOfPerProjectIsolated(t *testing.T) {

	withGlobalDefaultMode(t, i18n.SiteLangURLModeOff)

	project := stubLangModeProject{modes: map[string]string{
		"p-all-prefix": "all_prefix",
		"p-plain":      "default_plain",
		"p-bad":        "prefix_everything",
	}}
	ctx := context.Background()

	for round := 0; round < 3; round++ {
		if got := SiteLangURLModeOf(ctx, project, "p-all-prefix"); got != i18n.SiteLangURLModeAllPrefix {
			t.Fatalf("第 %d 轮：p-all-prefix 应解析为 all_prefix，实际 %q", round, got)
		}
		if got := SiteLangURLModeOf(ctx, project, "p-plain"); got != i18n.SiteLangURLModeDefaultPlain {
			t.Fatalf("第 %d 轮：p-plain 应解析为 default_plain，实际 %q", round, got)
		}
	}

	if got := SiteLangURLModeOf(ctx, project, "p-unset"); got != i18n.SiteLangURLModeOff {
		t.Fatalf("未配置的工程应回退全局默认（off），实际 %q", got)
	}
	if got := SiteLangURLModeOf(ctx, project, "p-bad"); got != i18n.SiteLangURLModeOff {
		t.Fatalf("非法取值应回退全局默认（off），实际 %q", got)
	}

	broken := stubLangModeProject{err: errors.New("数据库不可用")}
	if got := SiteLangURLModeOf(ctx, broken, "p-any"); got != i18n.SiteLangURLModeOff {
		t.Fatalf("读取失败应回退全局默认（off），实际 %q", got)
	}

	if got := SiteLangURLModeOf(ctx, nil, ""); got != i18n.SiteLangURLModeOff {
		t.Fatalf("无工程上下文应回退全局默认（off），实际 %q", got)
	}
}

func TestLangURLRuleFollowsProjectMode(t *testing.T) {
	withGlobalDefaultMode(t, i18n.SiteLangURLModeOff)

	project := stubLangModeProject{modes: map[string]string{
		"p-all-prefix": "all_prefix",
		"p-plain":      "default_plain",
	}}
	ctx := context.Background()

	allPrefix := LangURLRuleForProjectWithDefault(ctx, project, "p-all-prefix", "en-AU")
	if !allPrefix.Separated || !allPrefix.PrefixDefault {
		t.Fatalf("all_prefix 工程应「分离路径 + 默认语言带前缀」，实际 %+v", allPrefix)
	}
	plain := LangURLRuleForProjectWithDefault(ctx, project, "p-plain", "en-AU")
	if !plain.Separated || plain.PrefixDefault {
		t.Fatalf("default_plain 工程应「分离路径 + 默认语言不带前缀」，实际 %+v", plain)
	}
	if allPrefix.DefaultLang != "en-AU" || plain.DefaultLang != "en-AU" {
		t.Fatalf("默认语言应原样进规则：%+v / %+v", allPrefix, plain)
	}
}
