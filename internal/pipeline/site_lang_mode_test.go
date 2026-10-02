package pipeline

// site_lang_mode_test.go — 站点语言 URL 方案的**按工程解析**（多工程隔离）。
//
// 这批改动消灭的是「方案是进程级可变值」：以前 A 工程在设置页保存方案会写进 pkg/i18n 的
// 包级变量，于是 B 工程的构建/预览判定跟着变（同一进程里第二次解析拿到的是前一次的设置）。
// 这里钉住的正是替代它的判据：**每次解析都按 projectID 取**，工程之间不可能互相影响。

import (
	"context"
	"errors"
	"testing"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// stubLangModeProject 只回答「这个工程配了哪个方案」的最小工程服务。
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

// withGlobalDefaultMode 把**全局默认**方案（工程未配置时的兜底）打桩为给定值，用例结束复位。
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

// TestSiteLangURLModeOfPerProjectIsolated 两个工程各配不同方案：各自解析出自己的那份，
// 交替解析顺序不影响结果；未配置 / 非法 / 读失败一律回退全局默认方案。
func TestSiteLangURLModeOfPerProjectIsolated(t *testing.T) {
	// 全局默认刻意设成 off：任何「回退」都会表现为 off，与两个工程各自的配置区分得开。
	withGlobalDefaultMode(t, i18n.SiteLangURLModeOff)

	project := stubLangModeProject{modes: map[string]string{
		"p-all-prefix": "all_prefix",
		"p-plain":      "default_plain",
		"p-bad":        "prefix_everything", // 取值非法：回退全局默认
		// p-unset 刻意不在表里：未配置 → 回退全局默认
	}}
	ctx := context.Background()

	// 交替解析：若方案仍存在进程级状态（或解析被缓存），后一次会污染前一次的结果。
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
	// 无工程上下文（预览的少数入口 / nil 服务）：同样回退全局默认，不 panic。
	if got := SiteLangURLModeOf(ctx, nil, ""); got != i18n.SiteLangURLModeOff {
		t.Fatalf("无工程上下文应回退全局默认（off），实际 %q", got)
	}
}

// TestLangURLRuleFollowsProjectMode 规则真的跟着**工程**走：同一个默认语言、两个工程，
// 构造出的规则一个带全前缀、一个默认语言无前缀。
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
